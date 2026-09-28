// Промежуточные обёртки: access-лог, метрики и имя span по маршруту.
package main

import (
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// logRequests пишет по одной JSON-строке на запрос: метод, маршрут,
// код и задержка. Эти поля потом и ищутся в Loki.
func logRequests(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(recorder, r)

		route := routeOf(r)
		level := slog.LevelInfo
		if recorder.status >= http.StatusInternalServerError {
			level = slog.LevelError
		}

		logger.Log(r.Context(), level, "request",
			"method", r.Method,
			"route", route,
			"status", recorder.status,
			"duration_ms", float64(time.Since(started).Microseconds())/1000.0,
		)
	})
}

// traceHandler создаёт серверный span на каждый запрос.
func traceHandler(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, serviceName)
}

// renameSpan вызывается после того, как отработал роутер: только теперь
// известен шаблон маршрута, и span можно назвать «GET /info», а не «GET».
func renameSpan(r *http.Request, route string) {
	span := trace.SpanFromContext(r.Context())
	if !span.IsRecording() {
		return
	}
	span.SetName(r.Method + " " + route)
	span.SetAttributes(attribute.String("http.route", route))
}

func routeOf(r *http.Request) string {
	if r.Pattern == "" {
		return "unknown"
	}
	return r.Pattern
}
