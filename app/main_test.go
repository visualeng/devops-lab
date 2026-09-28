package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthz(t *testing.T) {
	rec := newRecorder()
	newHandler().ServeHTTP(rec, request(http.MethodGet, "/healthz"))

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ждали 200", rec.Code)
	}
	if got := decode(t, rec)["status"]; got != "ok" {
		t.Fatalf("status = %v, ждали ok", got)
	}
}

func TestInfoReportsBuildMetadata(t *testing.T) {
	oldVersion, oldCommit := version, commit
	t.Cleanup(func() { version, commit = oldVersion, oldCommit })

	version, commit = "1.2.3", "abc1234"

	rec := newRecorder()
	newHandler().ServeHTTP(rec, request(http.MethodGet, "/info"))

	body := decode(t, rec)
	if body["service"] != serviceName {
		t.Fatalf("service = %v, ждали %v", body["service"], serviceName)
	}
	if body["version"] != "1.2.3" || body["commit"] != "abc1234" {
		t.Fatalf("не отдали версию и коммит: %v", body)
	}
}

func TestRootOnlyMatchesExactPath(t *testing.T) {
	rec := newRecorder()
	newHandler().ServeHTTP(rec, request(http.MethodGet, "/nope"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d, ждали 404", rec.Code)
	}
}

func TestCheckHealth(t *testing.T) {
	server := httptest.NewServer(newHandler())
	defer server.Close()

	if err := checkHealth(server.URL + "/healthz"); err != nil {
		t.Fatalf("живой сервис должен проходить проверку: %v", err)
	}

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer broken.Close()

	if err := checkHealth(broken.URL + "/healthz"); err == nil {
		t.Fatal("сервис с 500 должен падать на healthcheck")
	}

	if err := checkHealth("http://127.0.0.1:1/healthz"); err == nil {
		t.Fatal("доступный порт должен падать на healthcheck")
	}
}

func TestHealthURL(t *testing.T) {
	cases := map[string]string{
		":8080":        "http://127.0.0.1:8080/healthz",
		"0.0.0.0:9000": "http://127.0.0.1:9000/healthz",
		"127.0.0.1:80": "http://127.0.0.1:80/healthz",
		"nonsense":     "http://127.0.0.1/healthz",
	}
	for addr, want := range cases {
		if got := healthURL(addr); got != want {
			t.Errorf("healthURL(%q) = %q, ждали %q", addr, got, want)
		}
	}
}

func request(method, path string) *http.Request {
	return httptest.NewRequest(method, path, nil)
}

func newRecorder() *httptest.ResponseRecorder {
	return httptest.NewRecorder()
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("ответ не JSON: %v (%s)", err, rec.Body.String())
	}
	return body
}
