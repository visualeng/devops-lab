// Логи, трассировки и middleware: то, что нужно для Loki и Tempo.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestLoggerWritesJSONWithBuildInfo(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(&buf)
	logger.Info("проверка", "route", "GET /healthz")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("строка лога не JSON: %v (%s)", err, buf.String())
	}
	if entry["service"] != serviceName {
		t.Fatalf("service = %v, ждали %v", entry["service"], serviceName)
	}
	if entry["version"] != version || entry["commit"] != commit {
		t.Fatalf("в логе нет версии и коммита: %v", entry)
	}
	if entry["msg"] != "проверка" || entry["route"] != "GET /healthz" {
		t.Fatalf("не отдали сообщение и маршрут: %v", entry)
	}
}

func TestLogRequestsRecordsRouteAndStatus(t *testing.T) {
	var buf bytes.Buffer
	handler := logRequests(newHandler(), newLogger(&buf))

	handler.ServeHTTP(newRecorder(), request(http.MethodGet, "/info"))

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("строка лога не JSON: %v (%s)", err, buf.String())
	}
	if entry["route"] != "GET /info" || entry["method"] != http.MethodGet {
		t.Fatalf("маршрут или метод не записаны: %v", entry)
	}
	if entry["status"] != float64(http.StatusOK) {
		t.Fatalf("status = %v, ждали 200", entry["status"])
	}
	if _, ok := entry["duration_ms"].(float64); !ok {
		t.Fatalf("duration_ms не число: %v", entry["duration_ms"])
	}
}

func TestLogRequestsMarksServerErrors(t *testing.T) {
	var buf bytes.Buffer
	failing := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	logRequests(failing, newLogger(&buf)).ServeHTTP(newRecorder(), request(http.MethodGet, "/boom"))

	if !strings.Contains(buf.String(), `"level":"ERROR"`) {
		t.Fatalf("5xx должен логироваться как ERROR: %s", buf.String())
	}
}

func TestWorkEndpointMakesChildSpan(t *testing.T) {
	t.Setenv("WORK_DELAY_MS", "1")

	rec := newRecorder()
	newHandler().ServeHTTP(rec, request(http.MethodGet, "/work"))

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ждали 200", rec.Code)
	}
	body := decode(t, rec)
	if body["slept_ms"] != float64(1) {
		t.Fatalf("slept_ms = %v, ждали 1", body["slept_ms"])
	}
}

func TestWorkDelayFallsBackToDefault(t *testing.T) {
	const fallback = 25 * time.Millisecond

	t.Setenv("WORK_DELAY_MS", "120")
	if got := workDelay(); got != 120*time.Millisecond {
		t.Fatalf("workDelay = %v, ждали 120ms", got)
	}

	for _, value := range []string{"", "не число", "-5", "99999"} {
		t.Setenv("WORK_DELAY_MS", value)
		if got := workDelay(); got != fallback {
			t.Fatalf("workDelay(%q) = %v, ждали %v", value, got, fallback)
		}
	}
}

func TestSetupTracingWithoutCollectorIsNoop(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	shutdown, err := setupTracing(context.Background())
	if err != nil {
		t.Fatalf("без коллектора ошибок быть не должно: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("остановка noop-провайдера: %v", err)
	}
}

func TestTracingStackDoesNotBreakWithoutExporter(t *testing.T) {
	// С коллектором, которого нет, span всё равно должен создаваться,
	// запрос — обслуживаться. Иначе стенд падал бы из-за Tempo.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")

	shutdown, err := setupTracing(context.Background())
	if err != nil {
		t.Fatalf("setupTracing: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = shutdown(ctx)
	})

	rec := newRecorder()
	traceHandler(newHandler()).ServeHTTP(rec, request(http.MethodGet, "/info"))

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ждали 200", rec.Code)
	}
}

func TestRenameSpanKeepsNoopSpanSilent(t *testing.T) {
	// С noop-провайдером (когда коллектор не настроен) переименование
	// должно быть тихой no-op, а не падением.
	ctx, span := noop.NewTracerProvider().Tracer("test").Start(context.Background(), "original")

	r := request(http.MethodGet, "/info")
	*r = *r.WithContext(trace.ContextWithSpan(ctx, span))

	renameSpan(r, "GET /info")

	if span.IsRecording() {
		t.Fatal("у span из noop-провайдера не должно быть записи")
	}
}

func TestSlogDefaultIsReplacedInMain(t *testing.T) {
	// main() ставит свой логгер через slog.SetDefault: если бы этого не
	// было, в логах не было бы service/version/commit.
	var buf bytes.Buffer
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	slog.SetDefault(newLogger(&buf))
	slog.Info("проверка")

	if !strings.Contains(buf.String(), `"commit":"`+commit+`"`) {
		t.Fatalf("глобальный логгер без метаданных сборки: %s", buf.String())
	}
}
