package main

import (
	"io"
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRequestsAreCounted(t *testing.T) {
	handler := instrument(newHandler())

	rec := newRecorder()
	handler.ServeHTTP(rec, request(http.MethodGet, "/healthz"))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ждали 200", rec.Code)
	}

	// Метка маршрута приходит из ServeMux вместе с методом
	if got := testutil.ToFloat64(
		httpRequests.WithLabelValues("GET /healthz", "200"),
	); got != 1 {
		t.Fatalf("счётчик /healthz = %v, ждали 1", got)
	}

	// тот же маршрут, но с кодом 404 считается отдельной серией
	rec = newRecorder()
	handler.ServeHTTP(rec, request(http.MethodGet, "/nope"))
	if got := testutil.ToFloat64(
		httpRequests.WithLabelValues("unknown", "404"),
	); got != 1 {
		t.Fatalf("счётчик 404 = %v, ждали 1", got)
	}
}

func TestBuildInfoExposesVersionAndCommit(t *testing.T) {
	rec := newRecorder()
	newHandler().ServeHTTP(rec, request(http.MethodGet, "/metrics"))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ждали 200", rec.Code)
	}

	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("не прочитали /metrics: %v", err)
	}

	want := `devops_lab_build_info{commit="` + commit + `",version="` + version + `"} 1`
	if !contains(string(body), want) {
		t.Fatalf("в /metrics нет %q", want)
	}
	if !contains(string(body), "devops_lab_http_request_duration_seconds") {
		t.Fatal("в /metrics нет гистограммы длительности")
	}
}

func TestMetricsEndpointIsNotCountedAsUnknown(t *testing.T) {
	// /metrics тоже ходит через instrument: лишняя метка не появляется,
	// потому что маршрут известен роутеру.
	handler := instrument(newHandler())
	handler.ServeHTTP(newRecorder(), request(http.MethodGet, "/metrics"))

	if got := testutil.ToFloat64(
		httpRequests.WithLabelValues("GET /metrics", "200"),
	); got != 1 {
		t.Fatalf("счётчик /metrics = %v, ждали 1", got)
	}
}
