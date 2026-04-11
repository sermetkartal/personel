package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/personel/gateway/internal/config"
)

// TracerProvider wraps the OTel SDK TracerProvider and exposes a Shutdown hook.
type TracerProvider struct {
	provider *sdktrace.TracerProvider
}

// InitTracing creates and registers a global OTel tracer provider that exports
// to the OTLP endpoint configured in cfg. Returns a provider whose Shutdown
// method must be deferred by the caller.
//
// If cfg.OTLPEndpoint is empty, a no-op tracer provider is installed.
func InitTracing(ctx context.Context, cfg config.ObservConfig) (*TracerProvider, error) {
	if cfg.OTLPEndpoint == "" {
		// No-op: install a no-op provider so instrumented code doesn't panic.
		otel.SetTracerProvider(trace.NewNoopTracerProvider())
		return &TracerProvider{}, nil
	}

	exp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("tracing: create OTLP exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("tracing: create resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return &TracerProvider{provider: tp}, nil
}

// Shutdown flushes and stops the tracer provider. Should be called on process exit.
func (t *TracerProvider) Shutdown(ctx context.Context) error {
	if t.provider == nil {
		return nil
	}
	return t.provider.Shutdown(ctx)
}
