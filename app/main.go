// Command app — крошечный сервис, который разворачивает лаборатория.
// Он специально простой: весь интерес репозитория в том, что происходит
// с ним дальше — образ, сканирование, стенд, деплой.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Проставляются линкером при сборке образа, см. app/Dockerfile.
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

const serviceName = "devops-lab"

func main() {
	addr := envOr("ADDR", ":8080")
	healthOnly := flag.Bool("healthcheck", false, "проверить собственный /healthz и выйти")
	flag.Parse()

	if *healthOnly {
		if err := checkHealth(healthURL(addr)); err != nil {
			slog.Error("healthcheck failed", "error", err)
			os.Exit(1)
		}
		return
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           newHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Слушаем и обрабатываем сигнал в одной горутине: если слушатель не
	// готов, а сигнал уже пришёл — старт не падает.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", addr, "version", version, "commit", commit)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			slog.Error("server stopped", "error", err)
			os.Exit(1)
		}
	case sig := <-stop:
		slog.Info("shutdown signal, draining", "signal", sig.String())
	}

	// Дренирование: даём текущим запросам дописать, но не залипаем вечно —
	// за это время оркестратор успеет переключить трафик.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("shutdown failed", "error", err)
		os.Exit(1)
	}
	slog.Info("stopped cleanly")
}

func newHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"service": serviceName,
			"version": version,
			"commit":  commit,
			"builtAt": buildTime,
		})
	})

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"service": serviceName,
			"message": "лаборатория DevOps: /healthz, /info",
		})
	})

	return mux
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("write response", "error", err)
	}
}

// checkHealth нужен для HEALTHCHECK в образе: в distroless нет ни shell,
// ни curl, поэтому проверяет сам бинарник.
func checkHealth(url string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("healthz вернул " + resp.Status)
	}
	return nil
}

// healthURL превращает ADDR (":8080" или "0.0.0.0:8080") в URL для проверки.
func healthURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://127.0.0.1/healthz"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz"
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
