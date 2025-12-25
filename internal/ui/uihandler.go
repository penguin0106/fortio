package ui

import (
	"context"
	"embed"
	"encoding/xml"
	"flag"
	"fmt"
	"html"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"fortio.org/dflag/endpoint"
	"fortio.org/duration"
	"fortio.org/fortio/internal/bincommon"
	"fortio.org/fortio/internal/metrics"
	"fortio.org/fortio/pkg/fhttp"
	"fortio.org/fortio/pkg/fnet"
	"fortio.org/fortio/pkg/periodic"
	"fortio.org/fortio/pkg/rapi"
	"fortio.org/fortio/pkg/stats"
	"fortio.org/fortio/pkg/version"
	"fortio.org/fortio/pkg/log"
)

// TODO: move some of those in their own files/package (e.g, data transfer TSV)
// and add unit tests.

var (
	//go:embed static/*
	staticFS embed.FS
	//go:embed templates/*
	templateFS embed.FS
)

var (
	// UI and Debug prefix/paths (read in UI handler).
	uiPath      string // absolute (base)
	logoPath    string // relative
	chartJSPath string // relative
	debugPath   string // absolute
	echoPath    string // absolute
	metricsPath string // absolute
	fetchPath   string // this one is absolute
	// Used to construct default URL to self.
	urlHostPort string
	// Start time of the UI Server (for uptime info).
	startTime        time.Time
	extraBrowseLabel string // Extra label for report only
	mainTemplate     *template.Template
	browseTemplate   *template.Template
	syncTemplate     *template.Template
)

const (
	fetchURI    = "fetch/"
	fetch2URI   = "fetch2/"
	faviconPath = "/favicon.ico"
)

// TODO: auto map from (Http)RunnerOptions to form generation and/or accept
// JSON serialized options as input.

// TODO: unit tests, allow additional data sets.

type mode int

// The main HTML has 3 principal modes.
const (
	// Default: renders the forms/menus.
	menu mode = iota
	// Trigger a run.
	run
	// Request abort.
	stop
)

// Handler is the main UI handler creating the web forms and processing them.
// TODO: refactor common option/args/flag parsing between restHandle.go and this.
//
//nolint:funlen, nestif // should be refactored indeed (TODO)
func Handler(w http.ResponseWriter, r *http.Request) {
	// logging of request and response is done by log.LogAndCall in mux setup
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		// method will be logged by LogAndCall so we just return (for HEAD etc... see Issue#830)
		return
	}
	mode := menu
	JSONOnly := false
	runid := int64(0)
	runner := r.FormValue("runner")
	httpopts := fhttp.CommonHTTPOptionsFromForm(r)
	url := httpopts.URL
	if r.FormValue("load") == "Start" {
		mode = run
		if r.FormValue("json") == "on" {
			JSONOnly = true
			log.Infof("Starting JSON only %s load request from %v for %s", runner, r.RemoteAddr, url)
		} else {
			log.Infof("Starting %s load request from %v for %s", runner, r.RemoteAddr, url)
		}
	} else if r.FormValue("stop") == "Stop" {
		runid, _ = strconv.ParseInt(r.FormValue("runid"), 10, 64)
		log.Critf("Stop request from %v for %d", r.RemoteAddr, runid)
		mode = stop
	}
	// Those only exist/make sense on run mode but go variable declaration...
	labels := r.FormValue("labels")
	resolution, _ := strconv.ParseFloat(r.FormValue("r"), 64)
	percList, _ := stats.ParsePercentiles(r.FormValue("p"))
	qps, _ := strconv.ParseFloat(r.FormValue("qps"), 64)
	durStr := r.FormValue("t")
	connectionReuseRange := parseConnectionReuseRange(
		r.FormValue("connection-reuse-range-min"),
		r.FormValue("connection-reuse-range-max"),
		r.FormValue("connection-reuse-range-value"))
	jitter := (r.FormValue("jitter") == "on")
	uniform := (r.FormValue("uniform") == "on")
	nocatchup := (r.FormValue("nocatchup") == "on")
	stdClient := (r.FormValue("stdclient") == "on")
	sequentialWarmup := (r.FormValue("sequential-warmup") == "on")
	var dur time.Duration
	if durStr == "on" || ((len(r.Form["t"]) > 1) && r.Form["t"][1] == "on") {
		dur = -1
	} else {
		var err error
		dur, err = time.ParseDuration(durStr)
		if mode == run && err != nil {
			log.Errf("Error parsing duration '%s': %v", durStr, err)
		}
	}
	c, _ := strconv.Atoi(r.FormValue("c"))
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Fatalf("expected http.ResponseWriter to be an http.Flusher")
	}
	out := io.Writer(os.Stderr)
	if len(percList) == 0 && !strings.Contains(r.URL.RawQuery, "p=") {
		percList = rapi.DefaultPercentileList
	}
	if !JSONOnly {
		out = fhttp.NewHTMLEscapeWriter(w)
	}
	n, _ := strconv.ParseInt(r.FormValue("n"), 10, 64)
	ro := periodic.RunnerOptions{
		QPS:         qps,
		Duration:    dur,
		Out:         out,
		NumThreads:  c,
		Resolution:  resolution,
		Percentiles: percList,
		Labels:      labels,
		Exactly:     n,
		Jitter:      jitter,
		Uniform:     uniform,
		NoCatchUp:   nocatchup,
	}
	if mode == run {
		// must not normalize, done in rapi.UpdateRun when actually starting the run
		runid = rapi.NextRunID()
		log.Infof("New run id %d", runid)
		ro.RunID = runid
	}
	httpopts.DisableFastClient = stdClient
	httpopts.SequentialWarmup = sequentialWarmup
	// Set the connection reuse range.
	err := bincommon.ConnectionReuseRange.
		WithValidator(bincommon.ConnectionReuseRangeValidator(httpopts)).
		Set(connectionReuseRange)
	if err != nil {
		log.Errf("Fail to validate connection reuse range flag, err: %v", err)
	}
	if !JSONOnly {
		// Normal HTML mode
		if mainTemplate == nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Critf("Nil template")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		durSeconds := dur.Seconds()
		if n > 0 {
			if qps > 0 {
				durSeconds = float64(n) / qps
			} else {
				durSeconds = -1
			}
			log.Infof("Estimating fixed #call %d duration to %g seconds %g", n, durSeconds, qps)
		}
		err := mainTemplate.Execute(w, &struct {
			R                           *http.Request
			Version                     string
			LongVersion                 string
			LogoPath                    string
			DebugPath                   string
			EchoDebugPath               string
			MetricsPath                 string
			ChartJSPath                 string
			StartTime                   string
			TargetURL                   string
			Labels                      string
			RunID                       int64
			UpTime                      duration.Duration
			TestExpectedDurationSeconds float64
			URLHostPort                 string
			DoStop                      bool
			DoLoad                      bool
		}{
			r, version.Short(), version.Long(), logoPath, debugPath, echoPath, metricsPath, chartJSPath,
			startTime.Format(time.ANSIC), url, labels, runid,
			fhttp.RoundDuration(time.Since(startTime)), durSeconds, urlHostPort, mode == stop, mode == run,
		})
		if err != nil {
			log.Critf("Template execution failed: %v", err)
		}
	}
	switch mode {
	case menu:
		// nothing more to do
	case stop:
		rapi.StopByRunID(runid, false)
	case run:
		// mode == run case:
		fhttp.OnBehalfOf(httpopts, r)
		runWriter := w
		if !JSONOnly {
			flusher.Flush()
			runWriter = nil // we don't want run to write json
		}

		// Calculate expected duration for progress monitoring
		expectedDuration := dur.Seconds()
		if n > 0 && qps > 0 {
			expectedDuration = float64(n) / qps
		}

		// Get consumer services from form (can be multiple)
		consumerServices := parseConsumerServicesFromForm(r)

		// Start progress monitoring
		stopMonitor := startRunMonitor(runid, ro.QPS, expectedDuration, runner, r.FormValue("kafka-topic"), consumerServices)

		// A bit awkward API because of trying to reuse yet be compatible from old UI code with
		// new `rapi` code.
		res, savedAs, json, err := rapi.Run(runWriter, r, nil, runner, url, &ro, httpopts, true /*HTML mode*/)

		// Stop monitoring and send final status
		if err != nil {
			stopMonitor("error")
		} else {
			stopMonitor("completed")
		}
		if err != nil {
			_, _ = fmt.Fprintf(w,
				"❌ Aborting because of %s\n</pre><script>document.getElementById('running').style.display = 'none';</script></body></html>\n",
				html.EscapeString(err.Error()))
			return
		}
		if JSONOnly {
			// all done in rapi.Run() above
			return
		}
		if savedAs != "" {
			id := res.Result().ID
			_, _ = fmt.Fprintf(w, "Saved result to <a href='%s'>%s</a>"+
				" (<a href='browse?url=%s.json' target='_new'>graph link</a>)\n", savedAs, savedAs, id)
		}
		_, _ = fmt.Fprintf(w, "All done %d calls %.3f ms avg, %.1f qps\n</pre>\n",
			res.Result().DurationHistogram.Count,
			1000.*res.Result().DurationHistogram.Avg,
			res.Result().ActualQPS)

		// Output summary cards
		_, _ = w.Write([]byte(`
<div id="resultsSummary" class="form-section" style="margin-top: 24px;">
  <h3 class="section-title">📊 Результаты теста</h3>
  <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 16px;">
    <div class="result-card">
      <div class="result-value" id="resTotal">` + fmt.Sprintf("%d", res.Result().DurationHistogram.Count) + `</div>
      <div class="result-label">Всего запросов</div>
    </div>
    <div class="result-card">
      <div class="result-value" id="resQPS">` + fmt.Sprintf("%.1f", res.Result().ActualQPS) + `</div>
      <div class="result-label">QPS</div>
    </div>
    <div class="result-card">
      <div class="result-value" id="resAvg">` + fmt.Sprintf("%.2f ms", 1000.*res.Result().DurationHistogram.Avg) + `</div>
      <div class="result-label">Avg Latency</div>
    </div>
    <div class="result-card">
      <div class="result-value" id="resMin">` + fmt.Sprintf("%.2f ms", 1000.*res.Result().DurationHistogram.Min) + `</div>
      <div class="result-label">Min Latency</div>
    </div>
    <div class="result-card">
      <div class="result-value" id="resMax">` + fmt.Sprintf("%.2f ms", 1000.*res.Result().DurationHistogram.Max) + `</div>
      <div class="result-label">Max Latency</div>
    </div>
    <div class="result-card">
      <div class="result-value" id="resDuration">` + fmt.Sprintf("%.1f s", res.Result().ActualDuration.Seconds()) + `</div>
      <div class="result-label">Длительность</div>
    </div>
  </div>
</div>
<style>
.result-card {
  background: linear-gradient(135deg, rgba(16, 185, 129, 0.1), rgba(59, 130, 246, 0.05));
  padding: 20px;
  border-radius: 12px;
  text-align: center;
  border: 1px solid var(--border, #e2e8f0);
}
.result-value {
  font-size: 1.75rem;
  font-weight: 700;
  color: var(--primary, #10b981);
}
.result-label {
  font-size: 0.9rem;
  color: var(--text-secondary, #64748b);
  margin-top: 6px;
}
</style>
<script>
`))
		ResultToJsData(w, json)
		_, _ = w.Write([]byte(`
// Parse Kafka metrics from pre content and display nicely
(function() {
  var preContent = document.querySelector('pre');
  if (!preContent) {
    console.log('No pre element found');
    return;
  }
  
  var text = preContent.textContent;
  console.log('Pre content length:', text.length);
  
  function formatBytes(bytes) {
    if (isNaN(bytes)) return bytes;
    if (bytes >= 1073741824) return (bytes / 1073741824).toFixed(2) + ' GB';
    if (bytes >= 1048576) return (bytes / 1048576).toFixed(2) + ' MB';
    if (bytes >= 1024) return (bytes / 1024).toFixed(2) + ' KB';
    return bytes + ' B';
  }
  
  var resultsSummary = document.getElementById('resultsSummary');
  var lastSection = resultsSummary;
  
  // Check if this is a Kafka test
  if (text.indexOf('Kafka Metrics:') !== -1) {
    console.log('Found Kafka Metrics in output');
    
    // Parse metrics
    var metrics = {};
    var lines = text.split('\n');
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i].trim();
      if (line.indexOf('Produce Requests Total:') === 0) metrics.total = line.split(':')[1].trim();
      if (line.indexOf('Produce Requests Success:') === 0) metrics.success = line.split(':')[1].trim();
      if (line.indexOf('Produce Requests Error:') === 0) metrics.errors = line.split(':')[1].trim();
      if (line.indexOf('Produce Bytes Total:') === 0) metrics.bytes = line.split(':')[1].trim();
      if (line.indexOf('Produce Latency Avg:') === 0) metrics.avgLatency = line.split(':')[1].trim();
      if (line.indexOf('Produce Latency Max:') === 0) metrics.maxLatency = line.split(':')[1].trim();
      if (line.indexOf('Total Messages sent:') === 0) metrics.messages = line.split(':')[1].trim();
    }
    
    console.log('Parsed Kafka metrics:', metrics);
    
    var metricsHtml = '';
    if (metrics.messages) metricsHtml += '<div class="result-card"><div class="result-value">' + metrics.messages + '</div><div class="result-label">Сообщений отправлено</div></div>';
    if (metrics.total) metricsHtml += '<div class="result-card"><div class="result-value">' + metrics.total + '</div><div class="result-label">Produce запросов</div></div>';
    if (metrics.success) metricsHtml += '<div class="result-card"><div class="result-value">' + metrics.success + '</div><div class="result-label">Успешных</div></div>';
    if (metrics.errors) metricsHtml += '<div class="result-card"><div class="result-value" style="color: ' + (parseInt(metrics.errors) > 0 ? '#ef4444' : '#10b981') + '">' + metrics.errors + '</div><div class="result-label">Ошибок</div></div>';
    if (metrics.bytes) metricsHtml += '<div class="result-card"><div class="result-value">' + formatBytes(parseInt(metrics.bytes)) + '</div><div class="result-label">Отправлено данных</div></div>';
    if (metrics.avgLatency) metricsHtml += '<div class="result-card"><div class="result-value">' + metrics.avgLatency + '</div><div class="result-label">Avg Latency</div></div>';
    if (metrics.maxLatency) metricsHtml += '<div class="result-card"><div class="result-value">' + metrics.maxLatency + '</div><div class="result-label">Max Latency</div></div>';
    
    if (metricsHtml && resultsSummary) {
      var kafkaDiv = document.createElement('div');
      kafkaDiv.className = 'form-section';
      kafkaDiv.id = 'kafkaMetricsSection';
      kafkaDiv.style.marginTop = '24px';
      kafkaDiv.innerHTML = '<h3 class="section-title">📨 Kafka Producer Metrics</h3>' +
        '<div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 16px;">' + metricsHtml + '</div>';
      resultsSummary.after(kafkaDiv);
      lastSection = kafkaDiv;
      console.log('Kafka metrics section added');
    }
  }
  
  // Check for Consumer metrics
  if (text.indexOf('Consumer Service Metrics:') !== -1) {
    console.log('Found Consumer Service Metrics in output');
    
    var metricsUrl = '';
    var metricsData = [];
    var inMetrics = false;
    var lines = text.split('\n');
    
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i].trim();
      if (line.indexOf('Metrics URL:') === 0) {
        metricsUrl = line.substring('Metrics URL:'.length).trim();
      }
      if (line.indexOf('Metrics Preview') === 0) {
        inMetrics = true;
        continue;
      }
      if (inMetrics && line && line.indexOf('Collection Error') === -1 && line.indexOf('Saved result') === -1) {
        metricsData.push(line);
      }
    }
    
    console.log('Consumer metrics URL:', metricsUrl);
    console.log('Consumer metrics data:', metricsData);
    
    if (metricsUrl) {
      var consumerDiv = document.createElement('div');
      consumerDiv.className = 'form-section';
      consumerDiv.id = 'consumerMetricsSection';
      consumerDiv.style.marginTop = '24px';
      
      // Parse consumer metrics into cards
      var consumerCards = '';
      for (var j = 0; j < metricsData.length; j++) {
        var metricLine = metricsData[j].trim();
        var parts = metricLine.split(/\s+/);
        if (parts.length >= 2) {
          var metricName = parts[0].replace(/_/g, ' ');
          var metricValue = parts[1];
          consumerCards += '<div class="result-card"><div class="result-value">' + metricValue + '</div><div class="result-label">' + metricName + '</div></div>';
        }
      }
      
      consumerDiv.innerHTML = '<h3 class="section-title">📥 Consumer Metrics</h3>' +
        '<p style="margin-bottom: 16px; color: var(--text-secondary, #64748b);"><strong>URL:</strong> ' + metricsUrl + '</p>' +
        (consumerCards ? '<div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 16px;">' + consumerCards + '</div>' : 
        '<pre style="background: var(--bg-secondary, #f1f5f9); padding: 16px; border-radius: 8px; font-size: 0.85rem; overflow-x: auto;">' + metricsData.join('\n') + '</pre>');
      
      lastSection.after(consumerDiv);
      console.log('Consumer metrics section added');
    }
  }
})();
</script>
<div style="text-align: center; margin: 32px 0;">
  <a href="./" style="
    display: inline-block;
    background: linear-gradient(135deg, #10b981, #06b6d4);
    color: white;
    padding: 16px 32px;
    border-radius: 12px;
    font-weight: 700;
    font-size: 1.1rem;
    text-decoration: none;
    box-shadow: 0 10px 25px -5px rgba(16, 185, 129, 0.4);
    transition: all 0.2s;
  " onmouseover="this.style.transform='translateY(-2px)'" onmouseout="this.style.transform='translateY(0)'">
    ← Назад к тестированию
  </a>
  <a href="browse" style="
    display: inline-block;
    background: var(--bg-card, #fff);
    color: var(--text-primary, #0f172a);
    padding: 16px 32px;
    border-radius: 12px;
    font-weight: 600;
    font-size: 1.1rem;
    text-decoration: none;
    border: 2px solid var(--border, #e2e8f0);
    margin-left: 12px;
    transition: all 0.2s;
  " onmouseover="this.style.borderColor='#10b981'" onmouseout="this.style.borderColor='var(--border, #e2e8f0)'">
    📊 Все результаты
  </a>
</div>
<div class="page-footer" style="text-align: center; padding: 24px; color: var(--text-muted, #94a3b8); margin-top: 32px;">
  <p>Fortio - <a href="https://fortio.org/" target="_blank">Документация</a> · <a href="https://github.com/fortio/fortio" target="_blank">GitHub</a></p>
</div>
</body></html>
`))
	}
}

// ResultToJsData converts a result object to chart data arrays and title
// and creates a chart from the result object.
func ResultToJsData(w io.Writer, json []byte) {
	_, _ = w.Write([]byte("var res = "))
	_, _ = w.Write(json)
	_, _ = w.Write([]byte("\nvar data = fortioResultToJsChartData(res)\nshowChart(data)\n"))
}

// SelectableValue represents an entry in the <select> of results.
type SelectableValue struct {
	Value    string
	Selected bool
}

// SelectValues maps the list of values (from DataList) to a list of SelectableValues.
// Each returned SelectableValue is selected if its value is contained in selectedValues.
// It is assumed that values does not contain duplicates.
func SelectValues(values []string, selectedValues []string) (selectableValues []SelectableValue, numSelected int) {
	set := make(map[string]bool, len(selectedValues))
	for _, selectedValue := range selectedValues {
		set[selectedValue] = true
	}

	for _, value := range values {
		_, selected := set[value]
		if selected {
			numSelected++
			delete(set, value)
		}
		selectableValue := SelectableValue{Value: value, Selected: selected}
		selectableValues = append(selectableValues, selectableValue)
	}
	return selectableValues, numSelected
}

// ChartOptions describes the user-configurable options for a chart.
type ChartOptions struct {
	XMin   string
	XMax   string
	YMin   string
	YMax   string
	XIsLog bool
	YIsLog bool
}

// BrowseHandler handles listing and rendering the JSON results.
func BrowseHandler(w http.ResponseWriter, r *http.Request) {
	// logging of request and response is done by log.LogAndCall in mux setup
	path := r.URL.Path
	if (path != uiPath) && (path != (uiPath + "browse")) {
		if strings.HasPrefix(path, "/fortio") {
			log.Infof("Redirecting /fortio in browse only path '%s'", path)
			http.Redirect(w, r, uiPath, http.StatusSeeOther)
		} else {
			log.Infof("Illegal browse path '%s'", path)
			w.WriteHeader(http.StatusNotFound)
		}
		return
	}
	url := r.FormValue("url")
	search := r.FormValue("s")
	xMin := r.FormValue("xMin")
	xMax := r.FormValue("xMax")
	// Ignore error, xLog == nil is the same as xLog being unspecified.
	xLog, _ := strconv.ParseBool(r.FormValue("xLog"))
	yMin := r.FormValue("yMin")
	yMax := r.FormValue("yMax")
	yLog, _ := strconv.ParseBool(r.FormValue("yLog"))
	dataList := rapi.DataList()
	selectedValues := r.URL.Query()["sel"]
	preselectedDataList, numSelected := SelectValues(dataList, selectedValues)

	doRender := url != ""
	doSearch := search != ""
	doLoadSelected := doSearch || numSelected > 0
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")

	chartOptions := ChartOptions{
		XMin:   xMin,
		XMax:   xMax,
		XIsLog: xLog,
		YMin:   yMin,
		YMax:   yMax,
		YIsLog: yLog,
	}
	err := browseTemplate.Execute(w, &struct {
		R                   *http.Request
		Extra               string
		Version             string
		LogoPath            string
		ChartJSPath         string
		URL                 string
		Search              string
		ChartOptions        ChartOptions
		PreselectedDataList []SelectableValue
		URLHostPort         string
		DoRender            bool
		DoSearch            bool
		DoLoadSelected      bool
	}{
		r, extraBrowseLabel, version.Short(), logoPath, chartJSPath,
		url, search, chartOptions, preselectedDataList, urlHostPort,
		doRender, doSearch, doLoadSelected,
	})
	if err != nil {
		log.Critf("Template execution failed: %v", err)
	}
}

// LogAndAddCacheControl logs the request and wraps an HTTP handler to add a Cache-Control header for static files.
func LogAndAddCacheControl(h http.Handler) http.Handler {
	return log.LogAndCall("static", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == faviconPath {
			r.URL.Path = "/static/img" + faviconPath // fortio/version expected to be stripped already
			log.LogVf("Changed favicon internal path to %s", r.URL.Path)
		}
		fhttp.CacheOn(w)
		h.ServeHTTP(w, r)
	})
}

// http.ResponseWriter + Flusher emulator - if we refactor the code this should
// not be needed. on the other hand it's useful and could be reused.
type outHTTPWriter struct {
	CodePtr *int // Needed because that interface is somehow pass by value
	Out     io.Writer
	header  http.Header
}

func (o outHTTPWriter) Header() http.Header {
	return o.header
}

func (o outHTTPWriter) Write(b []byte) (int, error) {
	return o.Out.Write(b)
}

func (o outHTTPWriter) WriteHeader(code int) {
	*o.CodePtr = code
	_, _ = fmt.Fprintf(o.Out, "\n*** result code: %d\n", code)
}

func (o outHTTPWriter) Flush() {
	// nothing
}

// Sync is the non-HTTP equivalent of fortio/sync?url=u.
func Sync(out io.Writer, u string, datadir string) bool {
	rapi.SetDataDir(datadir)
	v := url.Values{}
	v.Set("url", u)
	// TODO: better context?
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/sync-function?"+v.Encode(), nil)
	code := http.StatusOK // default
	w := outHTTPWriter{Out: out, CodePtr: &code}
	SyncHandler(w, req)
	return (code == http.StatusOK)
}

// SyncHandler handles syncing/downloading from TSC URL.
func SyncHandler(w http.ResponseWriter, r *http.Request) {
	// logging of request and response is done by log.LogAndCall in mux setup
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Fatalf("expected http.ResponseWriter to be an http.Flusher")
	}
	uStr := strings.TrimSpace(r.FormValue("url"))
	if syncTemplate != nil {
		err := syncTemplate.Execute(w, &struct {
			Version  string
			LogoPath string
			URL      string
		}{version.Short(), logoPath, uStr})
		if err != nil {
			log.Critf("Sync template execution failed: %v", err)
		}
	}
	_, _ = w.Write([]byte("Fetch of index/bucket url ... "))
	flusher.Flush()
	o := fhttp.NewHTTPOptions(uStr)
	fhttp.OnBehalfOf(o, r)
	// Increase timeout:
	o.HTTPReqTimeOut = 5 * time.Second
	// If we had hundreds of thousands of entry we should stream, parallelize (connection pool)
	// and not do multiple passes over the same data, but for small TSV this is fine.
	// use std client to avoid chunked raw we can get with fast client:
	client, _ := fhttp.NewStdClient(o) //nolint:contextcheck // yeah we should, possibly.
	if client == nil {
		_, _ = w.Write([]byte("invalid url!<script>setPB(1,1)</script></body></html>\n"))
		// too late to write headers for real case, but we do it anyway for the Sync() startup case
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	code, data, _ := client.Fetch(r.Context())
	defer client.Close()
	if code != http.StatusOK {
		_, _ = fmt.Fprintf(w, "http error, code %d<script>setPB(1,1)</script></body></html>\n", code)
		// too late to write headers for real case, but we do it anyway for the Sync() startup case
		w.WriteHeader(code)
		return
	}
	sdata := strings.TrimSpace(string(data))
	if strings.HasPrefix(sdata, "TsvHttpData-1.0") {
		processTSV(r.Context(), w, client, sdata)
	} else if !processXML(r.Context(), w, client, data, uStr, 0) {
		return
	}
	_, _ = w.Write([]byte("</table>"))
	_, _ = w.Write([]byte("\n</body></html>\n"))
}

func processTSV(ctx context.Context, w http.ResponseWriter, client *fhttp.Client, sdata string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Fatalf("processTSV expecting a flushable response")
	}
	lines := strings.Split(sdata, "\n")
	n := len(lines)

	_, _ = fmt.Fprintf(w, "success tsv fetch! Now fetching %d referenced URLs:<script>setPB(1,%d)</script>\n",
		n-1, n)
	_, _ = w.Write([]byte("<table>"))
	flusher.Flush()
	for i, l := range lines[1:] {
		parts := strings.Split(l, "\t")
		u := parts[0]
		_, _ = w.Write([]byte("<tr><td>"))
		_, _ = w.Write([]byte(template.HTMLEscapeString(u)))
		ur, err := url.Parse(u)
		if err != nil {
			_, _ = w.Write([]byte("<td>skipped (not a valid url)"))
		} else {
			uPath := ur.Path
			pathParts := strings.Split(uPath, "/")
			name := pathParts[len(pathParts)-1]
			downloadOne(ctx, w, client, name, u)
		}
		_, _ = fmt.Fprintf(w, "</tr><script>setPB(%d)</script>\n", i+2)
		flusher.Flush()
	}
	_, _ = w.Write([]byte("</table><p>All done!\n"))
}

// ListBucketResult is the minimum we need out of S3 XML results.
// https://docs.aws.amazon.com/AmazonS3/latest/API/RESTBucketGET.html
// e.g., https://storage.googleapis.com/fortio-data?max-keys=2&prefix=fortio.istio.io/
type ListBucketResult struct {
	NextMarker string   `xml:"NextMarker"`
	Names      []string `xml:"Contents>Key"`
}

// @returns true if started a table successfully - false is error.
func processXML(ctx context.Context, w http.ResponseWriter, client *fhttp.Client, data []byte, baseURL string, level int) bool {
	// We already know this parses as we just fetched it:
	bu, _ := url.Parse(baseURL)
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Fatalf("processXML expecting a flushable response")
	}
	l := ListBucketResult{}
	err := xml.Unmarshal(data, &l)
	if err != nil {
		log.Errf("xml unmarshal error %v", err)
		// don't show the error / would need HTML escape to avoid CSS attacks
		_, _ = w.Write([]byte("❌ xml parsing error, check logs<script>setPB(1,1)</script></body></html>\n"))
		w.WriteHeader(http.StatusInternalServerError)
		return false
	}
	n := len(l.Names)
	log.Infof("Parsed %+v", l)

	_, _ = fmt.Fprintf(w, "success xml fetch #%d! Now fetching %d referenced objects:<script>setPB(1,%d)</script>\n",
		level+1, n, n+1)
	if level == 0 {
		_, _ = w.Write([]byte("<table>"))
	}
	for i, el := range l.Names {
		_, _ = w.Write([]byte("<tr><td>"))
		_, _ = w.Write([]byte(template.HTMLEscapeString(el)))
		pathParts := strings.Split(el, "/")
		name := pathParts[len(pathParts)-1]
		newURL := *bu // copy
		newURL.Path = newURL.Path + "/" + el
		fullURL := newURL.String()
		downloadOne(ctx, w, client, name, fullURL)
		_, _ = fmt.Fprintf(w, "</tr><script>setPB(%d)</script>\n", i+2)
		flusher.Flush()
	}
	flusher.Flush()
	// Is there more data ? (NextMarker present)
	if len(l.NextMarker) == 0 {
		return true
	}
	if level > 100 {
		log.Errf("Too many chunks, stopping after 100")
		w.WriteHeader(509 /* Bandwidth Limit Exceeded */)
		return true
	}
	q := bu.Query()
	if q.Get("marker") == l.NextMarker {
		log.Errf("Loop with same marker %+v", bu)
		w.WriteHeader(http.StatusLoopDetected)
		return true
	}
	q.Set("marker", l.NextMarker)
	bu.RawQuery = q.Encode()
	newBaseURL := bu.String()
	// URL already validated
	_, _ = w.Write([]byte("<tr><td>"))
	_, _ = w.Write([]byte(template.HTMLEscapeString(newBaseURL)))
	_, _ = w.Write([]byte("<td>"))
	_ = client.ChangeURL(newBaseURL)
	ncode, ndata, _ := client.Fetch(ctx)
	if ncode != http.StatusOK {
		log.Errf("Can't fetch continuation with marker %+v", bu)

		_, _ = fmt.Fprintf(w, "❌ http error, code %d<script>setPB(1,1)</script></table></body></html>\n", ncode)
		w.WriteHeader(http.StatusFailedDependency)
		return false
	}
	return processXML(ctx, w, client, ndata, newBaseURL, level+1) // recurse
}

func downloadOne(ctx context.Context, w http.ResponseWriter, client *fhttp.Client, name string, u string) {
	log.Infof("downloadOne(%s,%s)", name, u)
	if !strings.HasSuffix(name, rapi.JSONExtension) {
		_, _ = w.Write([]byte("<td>skipped (not json)"))
		return
	}
	localPath := path.Join(rapi.GetDataDir(), name)
	_, err := os.Stat(localPath)
	if err == nil {
		_, _ = w.Write([]byte("<td>skipped (already exists)"))
		return
	}
	// note that if data dir doesn't exist this will trigger too - TODO: check datadir earlier
	if !os.IsNotExist(err) {
		log.Warnf("check %s : %v", localPath, err)
		// don't return the details of the error to not leak local data dir etc
		_, _ = w.Write([]byte("<td>❌ skipped (access error)"))
		return
	}
	// URL already validated
	_ = client.ChangeURL(u)
	code1, data1, _ := client.Fetch(ctx)
	if code1 != http.StatusOK {
		_, _ = fmt.Fprintf(w, "<td>❌ Http error, code %d", code1)
		w.WriteHeader(http.StatusFailedDependency)
		return
	}
	err = os.WriteFile(localPath, data1, 0o644) //nolint:gosec // we do want 644
	if err != nil {
		log.Errf("Unable to save %s: %v", localPath, err)
		_, _ = w.Write([]byte("<td>❌ skipped (write error)"))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	// finally ! success !
	log.Infof("Success fetching %s - saved at %s", u, localPath)
	// checkmark
	_, _ = w.Write([]byte("<td class='checkmark'>✓"))
}

func getMetricsPath(debugPath string) string {
	return strings.TrimSuffix(debugPath, "/") + "/metrics"
}

type ServerConfig struct {
	BaseURL, Port, DebugPath, UIPath, DataDir string
	PProfOn                                   bool
	PercentileList                            []float64
	TLSOptions                                *fhttp.TLSOptions
}

// Serve starts the fhttp.Serve() plus the UI server on the given port
// and paths (empty disables the feature). uiPath should end with /
// (be a 'directory' path). Returns true if server is started successfully.
func Serve(cfg *ServerConfig) bool {
	startTime = time.Now()
	mux, addr := fhttp.ServeTLS(cfg.Port, cfg.DebugPath, cfg.TLSOptions)
	if addr == nil {
		return false // Error already logged
	}
	if cfg.UIPath == "" {
		return true
	}
	if cfg.PProfOn {
		fhttp.SetupPPROF(mux) // This now logs a warning as it's a potential risk
	} else {
		log.LogVf("Not serving pprof endpoint.")
	}
	uiPath = cfg.UIPath
	if uiPath[len(uiPath)-1] != '/' {
		log.Warnf("Adding missing trailing / to UI path '%s'", uiPath)
		uiPath += "/"
	}
	debugPath = cfg.DebugPath
	echoPath = fhttp.EchoDebugPath(debugPath)
	metricsPath = getMetricsPath(debugPath)
	mux.HandleFunc(uiPath, log.LogAndCall("UI", Handler))
	fetchPath = uiPath + fetchURI
	// For backward compatibility with http:// only fetcher
	mux.Handle(fetchPath, http.StripPrefix(fetchPath, http.HandlerFunc(fhttp.FetcherHandler)))
	// h2 incoming and https outgoing ok fetcher
	mux.HandleFunc(uiPath+fetch2URI, fhttp.FetcherHandler2)
	fhttp.CheckConnectionClosedHeader = true // needed for proxy to avoid errors

	// New REST apis (includes the data/ handler)
	rapi.AddHandlers(mux, cfg.BaseURL, uiPath, cfg.DataDir)
	rapi.DefaultPercentileList = cfg.PercentileList

	logoPath = version.Short() + "/static/img/fortio-logo-gradient-no-bg.svg"
	chartJSPath = version.Short() + "/static/js/Chart.min.js"

	// Serve static contents in the ui/static dir. If not otherwise specified
	// by the function parameter staticPath, we use getResourcesDir which uses the
	// link time value or the directory relative to this file to find the static
	// contents, so no matter where or how the go binary is generated, the static
	// dir should be found.
	fs := http.FileServer(http.FS(staticFS))
	prefix := uiPath + version.Short()
	mux.Handle(prefix+"/static/", LogAndAddCacheControl(http.StripPrefix(prefix, fs)))
	mux.Handle(faviconPath, LogAndAddCacheControl(fs))
	var err error
	mainTemplate, err = template.ParseFS(templateFS, "templates/main.html", "templates/header.html")
	if err != nil {
		log.Critf("Unable to parse main template: %v", err)
	}
	browseTemplate, err = template.ParseFS(templateFS, "templates/browse.html", "templates/header.html")
	if err != nil {
		log.Critf("Unable to parse browse template: %v", err)
	} else {
		mux.HandleFunc(uiPath+"browse", log.LogAndCall("browse", BrowseHandler))
	}
	syncTemplate, err = template.ParseFS(templateFS, "templates/sync.html", "templates/header.html")
	if err != nil {
		log.Critf("Unable to parse sync template: %v", err)
	} else {
		mux.HandleFunc(uiPath+"sync", log.LogAndCall("Sync", SyncHandler))
	}

	// Real-time progress endpoints
	mux.HandleFunc(uiPath+"progress/sse", ProgressSSEHandler)
	mux.HandleFunc(uiPath+"progress/api", ProgressAPIHandler)

	dflagsPath := uiPath + "flags"
	dflagSetURL := dflagsPath + "/set"
	dflagEndPt := endpoint.NewFlagsEndpoint(flag.CommandLine, dflagSetURL)
	mux.HandleFunc(dflagsPath, dflagEndPt.ListFlags)
	mux.HandleFunc(dflagSetURL, dflagEndPt.SetFlag)

	// metrics endpoint
	log.Printf("Debug endpoint on %s, Additional Echo on %s, Flags on %s, and Metrics on %s",
		debugPath, echoPath, dflagsPath, metricsPath)
	mux.HandleFunc(metricsPath, metrics.Exporter)

	urlHostPort = fnet.NormalizeHostPort(cfg.Port, addr)
	uiMsg := "\t UI started - visit:\n\t\t"
	if strings.Contains(urlHostPort, "-unix-socket=") {
		uiMsg += fmt.Sprintf("fortio curl %s http://localhost%s", urlHostPort, uiPath)
	} else {
		isHTTPS := ""
		if cfg.TLSOptions.DoTLS() {
			isHTTPS = "s"
		}
		uiMsg += fmt.Sprintf("http%s://%s%s", isHTTPS, urlHostPort, uiPath)
		if strings.Contains(urlHostPort, "localhost") {
			uiMsg += "\n\t (or any host/ip reachable on this server)"
		}
	}
	fmt.Println(uiMsg)
	return true
}

// Report starts the browsing only UI server on the given port.
// Similar to Serve with only the read only part.
func Report(baseurl, port, datadir string) bool {
	// drop the pprof default handlers [shouldn't be needed with custom mux but better safe than sorry]
	http.DefaultServeMux = http.NewServeMux()
	extraBrowseLabel = ", report only limited UI"
	mux, addr := fhttp.HTTPServer("report", port)
	if addr == nil {
		return false
	}
	urlHostPort = fnet.NormalizeHostPort(port, addr)
	uiMsg := fmt.Sprintf("Browse only UI started - visit:\nhttp://%s/", urlHostPort)
	if !strings.Contains(port, ":") {
		uiMsg += "   (or any host/ip reachable on this server)"
	}
	fmt.Print(uiMsg + "\n")
	uiPath = "/"
	logoPath = version.Short() + "/static/img/fortio-logo-gradient-no-bg.svg"
	chartJSPath = version.Short() + "/static/js/Chart.min.js"
	fs := http.FileServer(http.FS(staticFS))
	prefix := uiPath + version.Short()
	mux.Handle(prefix+"/static/", LogAndAddCacheControl(http.StripPrefix(prefix, fs)))
	mux.Handle(faviconPath, LogAndAddCacheControl(fs))
	var err error
	browseTemplate, err = template.ParseFS(templateFS, "templates/browse.html", "templates/header.html")
	if err != nil {
		log.Critf("Unable to parse browse template: %v", err)
	} else {
		mux.HandleFunc(uiPath, BrowseHandler)
	}
	rapi.AddDataHandler(mux, baseurl, uiPath, datadir)
	return true
}

func parseConnectionReuseRange(minV string, maxV string, value string) string {
	if minV != "" && maxV != "" {
		return fmt.Sprintf("%s:%s", minV, maxV)
	} else if value != "" {
		return value
	}

	return ""
}

// parseConsumerServicesFromForm parses consumer services from form data
// Supports both services and lambda functions
func parseConsumerServicesFromForm(r *http.Request) []ConsumerServiceConfig {
	types := r.Form["consumer-type[]"]
	names := r.Form["consumer-name[]"]
	urls := r.Form["consumer-url[]"]
	functions := r.Form["consumer-function[]"]
	namespaces := r.Form["consumer-namespace[]"]
	autodiscovers := r.Form["consumer-autodiscover[]"]

	var services []ConsumerServiceConfig
	for i := 0; i < len(types); i++ {
		svcType := strings.TrimSpace(getFormValue(types, i))
		name := strings.TrimSpace(getFormValue(names, i))
		url := strings.TrimSpace(getFormValue(urls, i))

		if name == "" {
			continue
		}

		cfg := ConsumerServiceConfig{
			Type: svcType,
			Name: name,
		}

		if svcType == "function" {
			cfg.FunctionName = strings.TrimSpace(getFormValue(functions, i))
			cfg.Namespace = strings.TrimSpace(getFormValue(namespaces, i))
			cfg.AutoDiscover = getFormValue(autodiscovers, i) == "true"

			if cfg.FunctionName == "" {
				continue // Skip functions without name
			}

			// Resolve URL
			if cfg.AutoDiscover {
				resolvedURL, err := ResolveFunctionURL(cfg.FunctionName, "", true, cfg.Namespace)
				if err != nil {
					log.Warnf("Failed to auto-discover function %s: %v", cfg.FunctionName, err)
				} else {
					cfg.ResolvedURL = resolvedURL
					log.Infof("Auto-discovered function %s URL: %s", cfg.FunctionName, resolvedURL)
				}
			} else if url != "" {
				cfg.URL = url
				cfg.ResolvedURL = url
			}
		} else {
			// Service type
			if url == "" {
				continue // Skip services without URL
			}
			cfg.URL = url
			cfg.ResolvedURL = url
		}

		services = append(services, cfg)
	}
	return services
}

// getFormValue safely gets a value from a form array
func getFormValue(arr []string, idx int) string {
	if idx < len(arr) {
		return arr[idx]
	}
	return ""
}

// startRunMonitor starts a goroutine that monitors the run progress and sends updates via SSE
// Returns a function to call when the run completes
func startRunMonitor(runID int64, targetQPS float64, expectedSeconds float64, runType, kafkaTopic string, consumerServices []ConsumerServiceConfig) func(status string) {
	startTime := time.Now()
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})

	log.Infof("Starting progress monitor for run %d, expected %.1fs, type: %s, consumerServices: %d", runID, expectedSeconds, runType, len(consumerServices))

	// Time series data storage (keep last 200 points)
	const maxPoints = 200

	// Kafka metrics time series
	kafkaMetrics := make(map[string]*MetricTimeSeries)
	kafkaMetricColors := map[string]string{
		"qps":            "#10b981",
		"latency_avg":    "#3b82f6",
		"latency_max":    "#ef4444",
		"messages_total": "#8b5cf6",
		"bytes_total":    "#f59e0b",
		"success":        "#22c55e",
		"errors":         "#dc2626",
	}

	// Consumer metrics per service (serviceName -> metricName -> timeSeries)
	consumerServiceMetrics := make(map[string]map[string]*MetricTimeSeries)
	consumerMetricColors := []string{"#10b981", "#3b82f6", "#8b5cf6", "#f59e0b", "#ef4444", "#06b6d4", "#ec4899"}
	// Track color index per service
	serviceColorIndex := make(map[string]int)

	// Build ConsumerServices info for progress
	consumerServicesInfo := make([]ConsumerServiceInfo, len(consumerServices))
	for i, svc := range consumerServices {
		resolvedURL := svc.ResolvedURL
		if resolvedURL == "" {
			resolvedURL = svc.URL
		}
		consumerServicesInfo[i] = ConsumerServiceInfo{
			Type:     svc.Type,
			Name:     svc.Name,
			URL:      resolvedURL,
			Function: svc.FunctionName,
			Metrics:  []MetricTimeSeries{},
		}
		consumerServiceMetrics[svc.Name] = make(map[string]*MetricTimeSeries)
		serviceColorIndex[svc.Name] = 0
	}

	// Initialize progress immediately
	progress := &LiveProgress{
		RunID:            runID,
		Status:           "running",
		StartTime:        startTime,
		ExpectedSeconds:  expectedSeconds,
		TargetQPS:        targetQPS,
		KafkaTopic:       kafkaTopic,
		ConsumerServices: consumerServicesInfo,
	}
	UpdateProgress(runID, progress)

	go func() {
		defer close(doneCh)
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()

		consumerTicker := time.NewTicker(1 * time.Second) // Fetch consumer metrics every second
		defer consumerTicker.Stop()

		retryCount := 0
		var lastTotal int64

		for {
			select {
			case <-stopCh:
				return

			case <-consumerTicker.C:
				// Fetch consumer metrics from all configured services
				elapsed := time.Since(startTime).Seconds()
				for _, svc := range consumerServices {
					// Use ResolvedURL (which may be auto-discovered)
					metricsURL := svc.ResolvedURL
					if metricsURL == "" {
						metricsURL = svc.URL
					}
					if metricsURL == "" {
						continue // Skip if no URL available
					}
					metrics, err := FetchConsumerMetrics(metricsURL)
					if err == nil {
						svcMetrics := consumerServiceMetrics[svc.Name]
						colorIdx := serviceColorIndex[svc.Name]
						for _, m := range metrics {
							ts, exists := svcMetrics[m.Name]
							if !exists {
								ts = &MetricTimeSeries{
									Name:        m.Name,
									Label:       m.Name,
									Color:       consumerMetricColors[colorIdx%len(consumerMetricColors)],
									ServiceName: svc.Name,
									Points:      make([]TimeSeriesPoint, 0, maxPoints),
								}
								svcMetrics[m.Name] = ts
								colorIdx++
								serviceColorIndex[svc.Name] = colorIdx
							}
							ts.Points = appendPoint(ts.Points, TimeSeriesPoint{Time: elapsed, Value: m.Value}, maxPoints)
						}
					}
				}

			case <-ticker.C:
				elapsed := time.Since(startTime).Seconds()
				var progressPercent float64
				if expectedSeconds > 0 {
					progressPercent = (elapsed / expectedSeconds) * 100
					if progressPercent > 100 {
						progressPercent = 99
					}
				}

				// Try to get stats from the running test
				var total, success, errors int64
				var avgMs, minMs, maxMs float64
				var currentQPS float64

				liveStats := periodic.GetLiveStatsByRunID(runID)
				if liveStats != nil {
					total, success, errors, avgMs, minMs, maxMs = liveStats.GetSnapshot()
					if elapsed > 0.1 {
						currentQPS = float64(total) / elapsed
					}
					retryCount = 0

					// Add to Kafka metrics time series
					// QPS
					if kafkaMetrics["qps"] == nil {
						kafkaMetrics["qps"] = &MetricTimeSeries{Name: "qps", Label: "QPS", Unit: "req/s", Color: kafkaMetricColors["qps"], Points: make([]TimeSeriesPoint, 0, maxPoints)}
					}
					instantQPS := currentQPS
					if len(kafkaMetrics["qps"].Points) > 0 {
						lastPoint := kafkaMetrics["qps"].Points[len(kafkaMetrics["qps"].Points)-1]
						if elapsed > lastPoint.Time {
							dt := elapsed - lastPoint.Time
							instantQPS = float64(total-lastTotal) / dt
						}
					}
					kafkaMetrics["qps"].Points = appendPoint(kafkaMetrics["qps"].Points, TimeSeriesPoint{Time: elapsed, Value: instantQPS}, maxPoints)

					// Latency Avg
					if kafkaMetrics["latency_avg"] == nil {
						kafkaMetrics["latency_avg"] = &MetricTimeSeries{Name: "latency_avg", Label: "Avg Latency", Unit: "ms", Color: kafkaMetricColors["latency_avg"], Points: make([]TimeSeriesPoint, 0, maxPoints)}
					}
					kafkaMetrics["latency_avg"].Points = appendPoint(kafkaMetrics["latency_avg"].Points, TimeSeriesPoint{Time: elapsed, Value: avgMs}, maxPoints)

					// Latency Max
					if kafkaMetrics["latency_max"] == nil {
						kafkaMetrics["latency_max"] = &MetricTimeSeries{Name: "latency_max", Label: "Max Latency", Unit: "ms", Color: kafkaMetricColors["latency_max"], Points: make([]TimeSeriesPoint, 0, maxPoints)}
					}
					kafkaMetrics["latency_max"].Points = appendPoint(kafkaMetrics["latency_max"].Points, TimeSeriesPoint{Time: elapsed, Value: maxMs}, maxPoints)

					// Messages Total
					if kafkaMetrics["messages_total"] == nil {
						kafkaMetrics["messages_total"] = &MetricTimeSeries{Name: "messages_total", Label: "Messages Total", Unit: "count", Color: kafkaMetricColors["messages_total"], Points: make([]TimeSeriesPoint, 0, maxPoints)}
					}
					kafkaMetrics["messages_total"].Points = appendPoint(kafkaMetrics["messages_total"].Points, TimeSeriesPoint{Time: elapsed, Value: float64(total)}, maxPoints)

					// Success
					if kafkaMetrics["success"] == nil {
						kafkaMetrics["success"] = &MetricTimeSeries{Name: "success", Label: "Success", Unit: "count", Color: kafkaMetricColors["success"], Points: make([]TimeSeriesPoint, 0, maxPoints)}
					}
					kafkaMetrics["success"].Points = appendPoint(kafkaMetrics["success"].Points, TimeSeriesPoint{Time: elapsed, Value: float64(success)}, maxPoints)

					// Errors
					if kafkaMetrics["errors"] == nil {
						kafkaMetrics["errors"] = &MetricTimeSeries{Name: "errors", Label: "Errors", Unit: "count", Color: kafkaMetricColors["errors"], Points: make([]TimeSeriesPoint, 0, maxPoints)}
					}
					kafkaMetrics["errors"].Points = appendPoint(kafkaMetrics["errors"].Points, TimeSeriesPoint{Time: elapsed, Value: float64(errors)}, maxPoints)

					lastTotal = total
				} else {
					retryCount++
					if retryCount > 10 && retryCount%10 == 0 {
						log.LogVf("LiveStats not found for run %d after %d ticks", runID, retryCount)
					}
				}

				// Convert maps to slices for JSON
				kafkaMetricsSlice := make([]MetricTimeSeries, 0, len(kafkaMetrics))
				for _, v := range kafkaMetrics {
					kafkaMetricsSlice = append(kafkaMetricsSlice, *v)
				}

				// Build consumer services info with metrics
				consumerServicesSlice := make([]ConsumerServiceInfo, len(consumerServices))
				for i, svc := range consumerServices {
					svcMetrics := consumerServiceMetrics[svc.Name]
					metricsSlice := make([]MetricTimeSeries, 0, len(svcMetrics))
					for _, v := range svcMetrics {
						metricsSlice = append(metricsSlice, *v)
					}
					resolvedURL := svc.ResolvedURL
					if resolvedURL == "" {
						resolvedURL = svc.URL
					}
					consumerServicesSlice[i] = ConsumerServiceInfo{
						Type:     svc.Type,
						Name:     svc.Name,
						URL:      resolvedURL,
						Function: svc.FunctionName,
						Metrics:  metricsSlice,
					}
				}

				// Update progress
				newProgress := &LiveProgress{
					RunID:            runID,
					Status:           "running",
					StartTime:        startTime,
					ElapsedSeconds:   elapsed,
					ExpectedSeconds:  expectedSeconds,
					ProgressPercent:  progressPercent,
					RequestsTotal:    total,
					RequestsSuccess:  success,
					RequestsError:    errors,
					CurrentQPS:       currentQPS,
					TargetQPS:        targetQPS,
					LatencyAvg:       avgMs,
					LatencyMin:       minMs,
					LatencyMax:       maxMs,
					KafkaTopic:       kafkaTopic,
					KafkaMetrics:     kafkaMetricsSlice,
					ConsumerServices: consumerServicesSlice,
				}
				UpdateProgress(runID, newProgress)
			}
		}
	}()

	// Return stop function
	return func(finalStatus string) {
		close(stopCh)
		<-doneCh

		elapsed := time.Since(startTime).Seconds()

		var total, success, errors int64
		var avgMs, minMs, maxMs float64
		liveStats := periodic.GetLiveStatsByRunID(runID)
		if liveStats != nil {
			total, success, errors, avgMs, minMs, maxMs = liveStats.GetSnapshot()
		}

		var currentQPS float64
		if elapsed > 0 {
			currentQPS = float64(total) / elapsed
		}

		log.Infof("Progress monitor completed for run %d: %d requests, %.1f qps", runID, total, currentQPS)

		// Convert maps to slices
		kafkaMetricsSlice := make([]MetricTimeSeries, 0, len(kafkaMetrics))
		for _, v := range kafkaMetrics {
			kafkaMetricsSlice = append(kafkaMetricsSlice, *v)
		}

		// Build consumer services info with final metrics
		consumerServicesSlice := make([]ConsumerServiceInfo, len(consumerServices))
		for i, svc := range consumerServices {
			svcMetrics := consumerServiceMetrics[svc.Name]
			metricsSlice := make([]MetricTimeSeries, 0, len(svcMetrics))
			for _, v := range svcMetrics {
				metricsSlice = append(metricsSlice, *v)
			}
			resolvedURL := svc.ResolvedURL
			if resolvedURL == "" {
				resolvedURL = svc.URL
			}
			consumerServicesSlice[i] = ConsumerServiceInfo{
				Type:     svc.Type,
				Name:     svc.Name,
				URL:      resolvedURL,
				Function: svc.FunctionName,
				Metrics:  metricsSlice,
			}
		}

		finalProgress := &LiveProgress{
			RunID:            runID,
			Status:           finalStatus,
			StartTime:        startTime,
			ElapsedSeconds:   elapsed,
			ExpectedSeconds:  expectedSeconds,
			ProgressPercent:  100,
			RequestsTotal:    total,
			RequestsSuccess:  success,
			RequestsError:    errors,
			CurrentQPS:       currentQPS,
			TargetQPS:        targetQPS,
			LatencyAvg:       avgMs,
			LatencyMin:       minMs,
			LatencyMax:       maxMs,
			KafkaTopic:       kafkaTopic,
			KafkaMetrics:     kafkaMetricsSlice,
			ConsumerServices: consumerServicesSlice,
		}
		UpdateProgress(runID, finalProgress)

		// Clean up after delay
		go func() {
			time.Sleep(10 * time.Second)
			ClearProgress(runID)
			periodic.ClearLiveStatsByRunID(runID)
		}()
	}
}

// appendPoint adds a point to time series, keeping max size
func appendPoint(series []TimeSeriesPoint, point TimeSeriesPoint, maxSize int) []TimeSeriesPoint {
	series = append(series, point)
	if len(series) > maxSize {
		series = series[1:]
	}
	return series
}
