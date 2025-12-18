// Copyright 2025 Fortio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kafkarunner

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"fortio.org/fortio/periodic"
	"fortio.org/fortio/tcprunner"
	"fortio.org/log"
	"github.com/twmb/franz-go/pkg/kgo"
)

var (
	// KafkaURLPrefix is the URL prefix for triggering Kafka load.
	KafkaURLPrefix = "kafka://"
	// KafkaStatusOK is the map key on success.
	KafkaStatusOK = "OK"
	errProduce    = errors.New("produce error")
)

type KafkaResultMap map[string]int64

// RunnerResults is the aggregated result of a KafkaRunner.
// Also is the internal type used per thread/goroutine.
type RunnerResults struct {
	periodic.RunnerResults
	KafkaOptions
	RetCodes     KafkaResultMap
	MessagesSent int64
	BytesSent    int64
	client       *KafkaClient
	aborter      *periodic.Aborter
	// Kafka metrics (optional)
	KafkaMetrics *KafkaMetrics
}

// KafkaMetrics holds optional Kafka broker metrics
type KafkaMetrics struct {
	ProduceRequestsTotal   int64
	ProduceRequestsSuccess int64
	ProduceRequestsError   int64
	ProduceBytesTotal      int64
	ProduceLatencyAvg      time.Duration
	ProduceLatencyMax      time.Duration
	mu                     sync.Mutex
}

// Run tests Kafka message producing. Main call being run at the target QPS.
// To be set as the Function in RunnerOptions.
func (kafkastate *RunnerResults) Run(_ context.Context, t periodic.ThreadID) (bool, string) {
	log.Debugf("Calling in %d", t)
	err := kafkastate.client.Produce()
	if err != nil {
		errStr := err.Error()
		kafkastate.RetCodes[errStr]++
		return false, errStr
	}
	kafkastate.RetCodes[KafkaStatusOK]++
	return true, KafkaStatusOK
}

// KafkaOptions are options to the KafkaClient.
type KafkaOptions struct {
	BootstrapServers []string
	Topic            string
	Payload          []byte // what to send
	CollectMetrics   bool   // whether to collect Kafka metrics
}

// RunnerOptions includes the base RunnerOptions plus Kafka specific
// options.
type RunnerOptions struct {
	periodic.RunnerOptions
	KafkaOptions
}

// KafkaClient is the client used for Kafka message producing.
type KafkaClient struct {
	client       *kgo.Client
	topic        string
	req          []byte
	connID       int
	messageCount int64
	bytesSent    int64
	messagesSent int64
	doGenerate   bool
	metrics      *KafkaMetrics
}

// NewKafkaClient creates and initializes a Kafka client based on the KafkaOptions.
func NewKafkaClient(o *KafkaOptions) (*KafkaClient, error) {
	if len(o.BootstrapServers) == 0 {
		return nil, fmt.Errorf("bootstrap servers are required")
	}
	if o.Topic == "" {
		return nil, fmt.Errorf("topic is required")
	}

	opts := []kgo.Opt{
		kgo.SeedBrokers(o.BootstrapServers...),
		kgo.RequiredAcks(kgo.AllISRAcks()), // Wait for all in-sync replicas
		kgo.RecordDeliveryTimeout(5 * time.Second),
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}

	c := &KafkaClient{
		client:  client,
		topic:   o.Topic,
		req:     o.Payload,
		metrics: nil,
	}

	if len(c.req) == 0 {
		c.doGenerate = true
		c.req = tcprunner.GeneratePayload(0, 0)
	}

	if o.CollectMetrics {
		c.metrics = &KafkaMetrics{}
	}

	return c, nil
}

// Produce sends a message to Kafka.
func (c *KafkaClient) Produce() error {
	c.messageCount++
	var payload []byte
	if c.doGenerate {
		payload = tcprunner.GeneratePayload(c.connID, c.messageCount)
	} else {
		payload = c.req
	}

	record := &kgo.Record{
		Topic: c.topic,
		Value: payload,
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := c.client.ProduceSync(ctx, record)
	latency := time.Since(start)

	if result.FirstErr() != nil {
		if c.metrics != nil {
			c.metrics.mu.Lock()
			c.metrics.ProduceRequestsError++
			c.metrics.mu.Unlock()
		}
		return fmt.Errorf("%w: %v", errProduce, result.FirstErr())
	}

	c.messagesSent++
	c.bytesSent += int64(len(payload))

	if c.metrics != nil {
		c.metrics.mu.Lock()
		c.metrics.ProduceRequestsTotal++
		c.metrics.ProduceRequestsSuccess++
		c.metrics.ProduceBytesTotal += int64(len(payload))
		// Update latency metrics
		if c.metrics.ProduceLatencyAvg == 0 {
			c.metrics.ProduceLatencyAvg = latency
		} else {
			// Simple moving average approximation
			c.metrics.ProduceLatencyAvg = (c.metrics.ProduceLatencyAvg + latency) / 2
		}
		if latency > c.metrics.ProduceLatencyMax {
			c.metrics.ProduceLatencyMax = latency
		}
		c.metrics.mu.Unlock()
	}

	return nil
}

// Close closes the Kafka client and returns the total number of messages sent.
func (c *KafkaClient) Close() int64 {
	log.Debugf("Closing kafka client %p: topic %s, messages sent %d", c, c.topic, c.messagesSent)
	if c.client != nil {
		c.client.Close()
	}
	return c.messagesSent
}

// RunKafkaTest runs a Kafka test and returns the aggregated stats.
func RunKafkaTest(o *RunnerOptions) (*RunnerResults, error) {
	o.RunType = "Kafka"
	log.Infof("Starting kafka test for topic %s with %d threads at %.1f qps", o.Topic, o.NumThreads, o.QPS)
	r := periodic.NewPeriodicRunner(&o.RunnerOptions)
	defer r.Options().Abort()
	numThreads := r.Options().NumThreads
	out := r.Options().Out
	total := RunnerResults{
		aborter:  r.Options().Stop,
		RetCodes: make(KafkaResultMap),
	}
	total.Topic = o.Topic
	total.BootstrapServers = o.BootstrapServers
	total.CollectMetrics = o.CollectMetrics

	kafkastate := make([]RunnerResults, numThreads)
	var err error
	for i := range numThreads {
		r.Options().Runners[i] = &kafkastate[i]
		// Create a client for each 'thread'
		kafkastate[i].client, err = NewKafkaClient(&o.KafkaOptions)
		if kafkastate[i].client == nil {
			return nil, fmt.Errorf("unable to create client %d: %w", i, err)
		}
		kafkastate[i].client.connID = i
		if o.Exactly <= 0 {
			err := kafkastate[i].client.Produce()
			if i == 0 && log.LogVerbose() {
				log.LogVf("first message to topic %s: err %v", o.Topic, err)
			}
		}
		// Set up the stats for each 'thread'
		kafkastate[i].aborter = total.aborter
		kafkastate[i].RetCodes = make(KafkaResultMap)
	}

	total.RunnerResults = r.Run()

	// Aggregate results
	keys := []string{}
	for i := range numThreads {
		total.MessagesSent += kafkastate[i].client.Close()
		total.BytesSent += kafkastate[i].client.bytesSent
		for k := range kafkastate[i].RetCodes {
			if _, exists := total.RetCodes[k]; !exists {
				keys = append(keys, k)
			}
			total.RetCodes[k] += kafkastate[i].RetCodes[k]
		}
		// Aggregate metrics if enabled
		if o.CollectMetrics && kafkastate[i].client.metrics != nil {
			if total.KafkaMetrics == nil {
				total.KafkaMetrics = &KafkaMetrics{}
			}
			kafkastate[i].client.metrics.mu.Lock()
			total.KafkaMetrics.mu.Lock()
			total.KafkaMetrics.ProduceRequestsTotal += kafkastate[i].client.metrics.ProduceRequestsTotal
			total.KafkaMetrics.ProduceRequestsSuccess += kafkastate[i].client.metrics.ProduceRequestsSuccess
			total.KafkaMetrics.ProduceRequestsError += kafkastate[i].client.metrics.ProduceRequestsError
			total.KafkaMetrics.ProduceBytesTotal += kafkastate[i].client.metrics.ProduceBytesTotal
			if kafkastate[i].client.metrics.ProduceLatencyMax > total.KafkaMetrics.ProduceLatencyMax {
				total.KafkaMetrics.ProduceLatencyMax = kafkastate[i].client.metrics.ProduceLatencyMax
			}
			// Average latency calculation
			if total.KafkaMetrics.ProduceLatencyAvg == 0 {
				total.KafkaMetrics.ProduceLatencyAvg = kafkastate[i].client.metrics.ProduceLatencyAvg
			} else if kafkastate[i].client.metrics.ProduceLatencyAvg > 0 {
				total.KafkaMetrics.ProduceLatencyAvg = (total.KafkaMetrics.ProduceLatencyAvg + kafkastate[i].client.metrics.ProduceLatencyAvg) / 2
			}
			total.KafkaMetrics.mu.Unlock()
			kafkastate[i].client.metrics.mu.Unlock()
		}
	}

	// Cleanup state
	r.Options().ReleaseRunners()
	totalCount := float64(total.DurationHistogram.Count)
	_, _ = fmt.Fprintf(out, "Total Messages sent: %d\n", total.MessagesSent)
	_, _ = fmt.Fprintf(out, "Total Bytes sent: %d\n", total.BytesSent)
	sort.Strings(keys)
	for _, k := range keys {
		_, _ = fmt.Fprintf(out, "kafka %s : %d (%.1f %%)\n", k, total.RetCodes[k], 100.*float64(total.RetCodes[k])/totalCount)
	}

	// Print Kafka metrics if collected
	if total.KafkaMetrics != nil {
		total.KafkaMetrics.mu.Lock()
		_, _ = fmt.Fprintf(out, "\nKafka Metrics:\n")
		_, _ = fmt.Fprintf(out, "  Produce Requests Total: %d\n", total.KafkaMetrics.ProduceRequestsTotal)
		_, _ = fmt.Fprintf(out, "  Produce Requests Success: %d\n", total.KafkaMetrics.ProduceRequestsSuccess)
		_, _ = fmt.Fprintf(out, "  Produce Requests Error: %d\n", total.KafkaMetrics.ProduceRequestsError)
		_, _ = fmt.Fprintf(out, "  Produce Bytes Total: %d\n", total.KafkaMetrics.ProduceBytesTotal)
		_, _ = fmt.Fprintf(out, "  Produce Latency Avg: %v\n", total.KafkaMetrics.ProduceLatencyAvg)
		_, _ = fmt.Fprintf(out, "  Produce Latency Max: %v\n", total.KafkaMetrics.ProduceLatencyMax)
		total.KafkaMetrics.mu.Unlock()
	}

	return &total, nil
}
