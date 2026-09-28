// Метрики сервиса. Их читает Prometheus из /metrics, а на их основе
// observability/prometheus/rules.yml — правила алертов.
package main

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "devops_lab_http_requests_total",
		Help: "Сколько запросов обработано, по маршруту и коду ответа",
	}, []string{"handler", "code"})

	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "devops_lab_http_request_duration_seconds",
		Help:    "Длительность запроса по маршруту",
		Buckets: prometheus.DefBuckets,
	}, []string{"handler"})

	// Всегда 1, а версия и коммит лежат в метках: так график и алерты
	// сразу показывают, какой именно код запущен.
	buildInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "devops_lab_build_info",
		Help: "Информация о сборке: значение всегда 1, данные — в метках",
	}, []string{"version", "commit"})
)

func init() {
	buildInfo.WithLabelValues(version, commit).Set(1)
}

// instrument считает запросы и их длительность. Меткой маршрута берётся
// шаблон из ServeMux ("/api/{id}"), а не сам путь: иначе в метках
// окажется каждый конкретный id и cardinality взорвётся.
func instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(recorder, r)

		handler := r.Pattern
		if handler == "" {
			handler = "unknown"
		}
		httpRequests.WithLabelValues(handler, strconv.Itoa(recorder.status)).Inc()
		httpDuration.WithLabelValues(handler).Observe(time.Since(started).Seconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
