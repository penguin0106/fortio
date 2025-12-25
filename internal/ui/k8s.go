package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"fortio.org/fortio/pkg/log"
)

const (
	// DefaultFunctionPort is the default port for lambda function metrics
	DefaultFunctionPort = "8888"
	// FunctionNamespaceEnv is the environment variable for function namespace
	FunctionNamespaceEnv = "FUNCTION_NAMESPACE"
	// DefaultFunctionNamespace if env not set
	DefaultFunctionNamespace = "default"
)

// PodInfo holds information about a Kubernetes pod
type PodInfo struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	PodIP     string `json:"podIP"`
	Status    string `json:"status"`
}

// K8sClient provides methods to interact with Kubernetes API
type K8sClient struct {
	host      string
	token     string
	caCert    string
	namespace string
	client    *http.Client
}

// NewK8sClient creates a new Kubernetes client using in-cluster config
func NewK8sClient() (*K8sClient, error) {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, fmt.Errorf("not running in Kubernetes cluster (KUBERNETES_SERVICE_HOST/PORT not set)")
	}

	tokenBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return nil, fmt.Errorf("failed to read service account token: %w", err)
	}

	namespace, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		// Use default namespace from env or fallback
		ns := os.Getenv(FunctionNamespaceEnv)
		if ns == "" {
			ns = DefaultFunctionNamespace
		}
		namespace = []byte(ns)
	}

	return &K8sClient{
		host:      fmt.Sprintf("https://%s:%s", host, port),
		token:     string(tokenBytes),
		namespace: string(namespace),
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: nil, // In production, load CA cert
			},
		},
	}, nil
}

// GetFunctionNamespace returns the namespace for functions from env or default
func GetFunctionNamespace() string {
	ns := os.Getenv(FunctionNamespaceEnv)
	if ns == "" {
		return DefaultFunctionNamespace
	}
	return ns
}

// GetPodByLabelSelector finds pods by label selector
func (c *K8sClient) GetPodByLabelSelector(namespace, labelSelector string) ([]PodInfo, error) {
	if namespace == "" {
		namespace = GetFunctionNamespace()
	}

	url := fmt.Sprintf("%s/api/v1/namespaces/%s/pods?labelSelector=%s", c.host, namespace, labelSelector)

	req, err := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("K8s API error: %d - %s", resp.StatusCode, string(body))
	}

	var podList struct {
		Items []struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Status struct {
				PodIP string `json:"podIP"`
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&podList); err != nil {
		return nil, err
	}

	var pods []PodInfo
	for _, item := range podList.Items {
		pods = append(pods, PodInfo{
			Name:      item.Metadata.Name,
			Namespace: item.Metadata.Namespace,
			PodIP:     item.Status.PodIP,
			Status:    item.Status.Phase,
		})
	}
	return pods, nil
}

// GetFunctionPodIP gets the IP of a pod running a specific function
// functionName is used to build label selector: function=<functionName>
func (c *K8sClient) GetFunctionPodIP(functionName string, namespace string) (string, error) {
	if namespace == "" {
		namespace = GetFunctionNamespace()
	}

	// Try common label selectors for serverless functions
	labelSelectors := []string{
		fmt.Sprintf("function=%s", functionName),
		fmt.Sprintf("faas_function=%s", functionName),
		fmt.Sprintf("app=%s", functionName),
		fmt.Sprintf("app.kubernetes.io/name=%s", functionName),
	}

	for _, selector := range labelSelectors {
		pods, err := c.GetPodByLabelSelector(namespace, selector)
		if err != nil {
			log.LogVf("Failed to get pods with selector %s: %v", selector, err)
			continue
		}
		for _, pod := range pods {
			if pod.PodIP != "" && pod.Status == "Running" {
				log.Infof("Found function %s pod IP: %s (selector: %s)", functionName, pod.PodIP, selector)
				return pod.PodIP, nil
			}
		}
	}

	return "", fmt.Errorf("no running pod found for function %s in namespace %s", functionName, namespace)
}

// BuildFunctionMetricsURL builds the metrics URL for a function
func BuildFunctionMetricsURL(podIP string, port string) string {
	if port == "" {
		port = DefaultFunctionPort
	}
	return fmt.Sprintf("http://%s:%s/metrics", podIP, port)
}

// Global K8s client instance (lazy initialized)
var globalK8sClient *K8sClient
var k8sClientError error
var k8sClientInitialized bool

// GetK8sClient returns the global K8s client (or error if not in cluster)
func GetK8sClient() (*K8sClient, error) {
	if !k8sClientInitialized {
		globalK8sClient, k8sClientError = NewK8sClient()
		k8sClientInitialized = true
		if k8sClientError != nil {
			log.Infof("K8s client not available: %v", k8sClientError)
		} else {
			log.Infof("K8s client initialized, function namespace: %s", GetFunctionNamespace())
		}
	}
	return globalK8sClient, k8sClientError
}

// ResolveFunctionURL resolves the metrics URL for a function
// If autoDiscover is true and we're in K8s, try to get pod IP automatically
func ResolveFunctionURL(functionName string, manualURL string, autoDiscover bool, namespace string) (string, error) {
	// If manual URL provided, use it
	if manualURL != "" {
		// Ensure it has /metrics suffix
		if !strings.HasSuffix(manualURL, "/metrics") && !strings.Contains(manualURL, "/metrics") {
			if !strings.HasSuffix(manualURL, "/") {
				manualURL += "/"
			}
			manualURL += "metrics"
		}
		return manualURL, nil
	}

	// If not auto-discover, return empty
	if !autoDiscover || functionName == "" {
		return "", nil
	}

	// Try to auto-discover using K8s
	client, err := GetK8sClient()
	if err != nil {
		return "", fmt.Errorf("auto-discovery not available: %w", err)
	}

	podIP, err := client.GetFunctionPodIP(functionName, namespace)
	if err != nil {
		return "", err
	}

	return BuildFunctionMetricsURL(podIP, DefaultFunctionPort), nil
}

// MetricsSourceType defines the type of metrics source
type MetricsSourceType string

const (
	MetricsSourceService  MetricsSourceType = "service"
	MetricsSourceFunction MetricsSourceType = "function"
)

// MetricsSource represents a source for collecting metrics
type MetricsSource struct {
	Type         MetricsSourceType `json:"type"`
	Name         string            `json:"name"`
	URL          string            `json:"url,omitempty"`          // For service or manual URL
	FunctionName string            `json:"functionName,omitempty"` // For lambda function
	Namespace    string            `json:"namespace,omitempty"`    // K8s namespace for function
	AutoDiscover bool              `json:"autoDiscover,omitempty"` // Auto-discover function pod IP
	ResolvedURL  string            `json:"resolvedUrl,omitempty"`  // Resolved URL (after discovery)
}

// Resolve resolves the actual URL for this metrics source
func (m *MetricsSource) Resolve() error {
	if m.Type == MetricsSourceService {
		m.ResolvedURL = m.URL
		return nil
	}

	// Function type
	url, err := ResolveFunctionURL(m.FunctionName, m.URL, m.AutoDiscover, m.Namespace)
	if err != nil {
		return err
	}
	m.ResolvedURL = url
	return nil
}
