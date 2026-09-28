// Трассировки: OTLP в Tempo, если адрес коллектора задан. Без адреса
// остаётся noop-провайдер, и сервис работает как ни в чём не бывало —
// стенд не должен падать из-за того, что Tempo ещё не поднят.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	"go.opentelemetry.io/otel/trace"
)

// setupTracing возвращает функцию остановки: её зовём после shutdown
// сервера, чтобы дослать неотправленные span'ы.
func setupTracing(ctx context.Context) (func(context.Context) error, error) {
	if strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")) == "" {
		return func(context.Context) error { return nil }, nil
	}

	// Адрес и таймауты экспортер читает из OTEL_EXPORTER_OTLP_* сам
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("otlp exporter: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version),
			attribute.String("service.commit", commit),
		)),
		// В лаборатории сэмплируем всё: иначе половина трейсов просто
		// не появится и разбираться будет не с чем.
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return provider.Shutdown, nil
}

func tracer() trace.Tracer {
	return otel.Tracer(serviceName)
}
