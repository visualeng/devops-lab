// Command app — крошечный сервис, который разворачивает лаборатория.
// Он специально простой: весь интерес репозитория в том, что происходит
// с ним дальше — образ, сканирование, стенд, деплой, метрики, логи и трейсы.
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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/attribute"
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

	logger := newLogger(os.Stdout)
	slog.SetDefault(logger)

	if *healthOnly {
		if err := checkHealth(healthURL(addr)); err != nil {
			logger.Error("healthcheck failed", "error", err)
			os.Exit(1)
		}
		return
	}

	shutdownTracing, err := setupTracing(context.Background())
	if err != nil {
		logger.Error("tracing setup failed", "error", err)
		os.Exit(1)
	}
	logger.Info("tracing ready")

	// Порядок обёрток: снаружи трейсинг (чтобы в логах был trace_id),
	// потом метрики с переименованием span, потом access-лог.
	handler := traceHandler(instrument(logRequests(newHandler(), logger)))
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Слушаем и обрабатываем сигнал в одной горутине: если слушатель не
	// готов, а сигнал уже пришёл — старт не падает.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", addr, "built_at", buildTime)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			logger.Error("server stopped", "error", err)
			os.Exit(1)
		}
	case sig := <-stop:
		logger.Info("shutdown signal, draining", "signal", sig.String())
	}

	// Дренирование: даём текущим запросам дописать, но не залипаем вечно —
	// за это время оркестратор успеет переключить трафик.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("shutdown failed", "error", err)
		os.Exit(1)
	}

	flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer flushCancel()
	if err := shutdownTracing(flushCtx); err != nil {
		logger.Error("flush traces", "error", err)
	}
	logger.Info("stopped cleanly")
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

	// Ручка для трейсов: внутри живёт дочерний span «downstream.pricing».
	// Так в Tempo видно, где именно копится время, а не только «запрос был».
	mux.HandleFunc("GET /work", func(w http.ResponseWriter, r *http.Request) {
		delay := workDelay()

		_, span := tracer().Start(r.Context(), "downstream.pricing")
		span.SetAttributes(
			attribute.String("downstream.name", "pricing"),
			attribute.Int64("downstream.delay_ms", delay.Milliseconds()),
		)
		time.Sleep(delay)
		span.End()

		logger := slog.Default()
		logger.Info("downstream call", "downstream", "pricing", "delay_ms", delay.Milliseconds())

		writeJSON(w, http.StatusOK, map[string]any{
			"service":  serviceName,
			"slept_ms": delay.Milliseconds(),
		})
	})

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"service": serviceName,
			"message": "лаборатория DevOps: /healthz, /info, /work, /metrics",
		})
	})

	// Метрики для Prometheus: счётчики, гистограмма и информация о сборке.
	mux.Handle("GET /metrics", promhttp.Handler())

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

// workDelay — искусственная задержка «внешнего» вызова, чтобы было что
// смотреть в трейсах и на графике задержек.
func workDelay() time.Duration {
	const fallback = 25 * time.Millisecond
	const limit = 5 * time.Second

	ms, err := strconv.Atoi(strings.TrimSpace(os.Getenv("WORK_DELAY_MS")))
	if err != nil {
		return fallback
	}
	delay := time.Duration(ms) * time.Millisecond
	if delay < 0 || delay > limit {
		return fallback
	}
	return delay
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
