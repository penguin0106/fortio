// Пакет metrics предоставляет минимальный пакет экспорта метрик для Fortio.
package metrics // import "fortio.org/fortio/metrics"

import (
	"io"
	"net/http"
	"runtime"
	"strconv"

	"fortio.org/fortio/rapi"
	"fortio.org/log"
	"fortio.org/scli"
)

// Exporter записывает минимальные метрики в стиле prometheus в http.ResponseWriter.
func Exporter(w http.ResponseWriter, r *http.Request) {
	log.LogRequest(r, "metrics")
	w.Header().Set("Content-Type", "text/plain")
	_, _ = io.WriteString(w, `# HELP fortio_num_fd Количество открытых файловых дескрипторов
# TYPE fortio_num_fd gauge
fortio_num_fd `)
	_, _ = io.WriteString(w, strconv.Itoa(scli.NumFD()))
	cur, total := rapi.RunMetrics()
	_, _ = io.WriteString(w, `
# HELP fortio_running Количество текущих нагрузочных тестов
# TYPE fortio_running gauge
fortio_running `)
	_, _ = io.WriteString(w, strconv.Itoa(cur))
	_, _ = io.WriteString(w, `
# HELP fortio_runs_total Общее количество запусков
# TYPE fortio_runs_total counter
fortio_runs_total `)
	_, _ = io.WriteString(w, strconv.FormatInt(total, 10))
	_, _ = io.WriteString(w, `
# HELP fortio_goroutines Текущее количество горутин
# TYPE fortio_goroutines gauge
fortio_goroutines `)
	_, _ = io.WriteString(w, strconv.FormatInt(int64(runtime.NumGoroutine()), 10))
	_, _ = io.WriteString(w, "\n")
}
