/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tracing

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	// TracerName is the name of the Kortex tracer
	TracerName = "github.com/judeoyovbaire/kortex"

	// ServiceName is the default service name for traces
	ServiceName = "kortex-gateway"
)

// Config holds tracing configuration
type Config struct {
	// Enabled determines if tracing is active
	Enabled bool

	// Endpoint is the OTLP collector endpoint (e.g., "localhost:4317")
	Endpoint string

	// ServiceName overrides the default service name
	ServiceName string

	// ServiceVersion is the version of the service
	ServiceVersion string

	// SampleRate is the fraction of traces to sample (0.0 to 1.0)
	SampleRate float64

	// Insecure disables TLS for the OTLP connection
	Insecure bool
}

// DefaultConfig returns the default tracing configuration
func DefaultConfig() Config {
	return Config{
		Enabled:        false,
		Endpoint:       "localhost:4317",
		ServiceName:    ServiceName,
		ServiceVersion: "v0.1.0",
		SampleRate:     1.0,
		Insecure:       true,
	}
}

// Tracer wraps OpenTelemetry tracing functionality for Kortex
type Tracer struct {
	provider *sdktrace.TracerProvider
	tracer   trace.Tracer
	config   Config
}

// NewTracer creates a new Tracer instance
func NewTracer(cfg Config) (*Tracer, error) {
	if !cfg.Enabled {
		// Return a no-op tracer when disabled
		return &Tracer{
			tracer: otel.Tracer(TracerName),
			config: cfg,
		}, nil
	}

	ctx := context.Background()

	// Create OTLP exporter options
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
	}

	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	// Create the OTLP exporter
	exporter, err := otlptrace.New(ctx, otlptracegrpc.NewClient(opts...))
	if err != nil {
		return nil, err
	}

	// Create resource with service information
	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = ServiceName
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			attribute.String("deployment.environment", "production"),
		),
	)
	if err != nil {
		return nil, err
	}

	// Create sampler based on sample rate
	var sampler sdktrace.Sampler
	if cfg.SampleRate >= 1.0 {
		sampler = sdktrace.AlwaysSample()
	} else if cfg.SampleRate <= 0 {
		sampler = sdktrace.NeverSample()
	} else {
		sampler = sdktrace.TraceIDRatioBased(cfg.SampleRate)
	}

	// Create trace provider
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Set global trace provider
	otel.SetTracerProvider(provider)

	// Set global propagator for context propagation
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &Tracer{
		provider: provider,
		tracer:   provider.Tracer(TracerName),
		config:   cfg,
	}, nil
}

// Shutdown gracefully shuts down the tracer
func (t *Tracer) Shutdown(ctx context.Context) error {
	if t.provider == nil {
		return nil
	}
	return t.provider.Shutdown(ctx)
}

// StartSpan starts a new span with the given name
func (t *Tracer) StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, name, opts...)
}

// StartRequestSpan starts a span for an incoming HTTP request
func (t *Tracer) StartRequestSpan(ctx context.Context, r *http.Request) (context.Context, trace.Span) {
	// Extract context from incoming request headers
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(r.Header))

	ctx, span := t.tracer.Start(ctx, "kortex.request",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			semconv.HTTPMethod(r.Method),
			semconv.HTTPTarget(r.URL.Path),
			semconv.HTTPScheme(r.URL.Scheme),
			semconv.NetHostName(r.Host),
			attribute.String("http.user_agent", r.UserAgent()),
			attribute.String("http.remote_addr", r.RemoteAddr),
		),
	)

	// Extract custom headers
	if route := r.Header.Get("X-Route"); route != "" {
		span.SetAttributes(attribute.String("kortex.route", route))
	}
	if model := r.Header.Get("X-Model"); model != "" {
		span.SetAttributes(attribute.String("kortex.model", model))
	}
	if userID := r.Header.Get("X-User-ID"); userID != "" {
		span.SetAttributes(attribute.String("kortex.user_id", userID))
	}

	return ctx, span
}

// StartBackendSpan starts a span for a backend request
func (t *Tracer) StartBackendSpan(ctx context.Context, backendName, backendType, targetURL string) (context.Context, trace.Span) {
	ctx, span := t.tracer.Start(ctx, "kortex.backend.request",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("kortex.backend.name", backendName),
			attribute.String("kortex.backend.type", backendType),
			attribute.String("kortex.backend.url", targetURL),
		),
	)
	return ctx, span
}

// StartRouterSpan starts a span for routing decision
func (t *Tracer) StartRouterSpan(ctx context.Context, routeName string) (context.Context, trace.Span) {
	ctx, span := t.tracer.Start(ctx, "kortex.router.route",
		trace.WithAttributes(
			attribute.String("kortex.route.name", routeName),
		),
	)
	return ctx, span
}

// StartSmartRouterSpan starts a span for smart routing decisions
func (t *Tracer) StartSmartRouterSpan(ctx context.Context, backend, category, reason string, estimatedTokens int) (context.Context, trace.Span) {
	ctx, span := t.tracer.Start(ctx, "kortex.smartrouter.decision",
		trace.WithAttributes(
			attribute.String("kortex.smartrouter.backend", backend),
			attribute.String("kortex.smartrouter.category", category),
			attribute.String("kortex.smartrouter.reason", reason),
			attribute.Int("kortex.smartrouter.estimated_tokens", estimatedTokens),
		),
	)
	return ctx, span
}

// SetSpanError marks a span as having an error
func SetSpanError(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// SetSpanOK marks a span as successful
func SetSpanOK(span trace.Span) {
	span.SetStatus(codes.Ok, "")
}

// AddBackendAttributes adds backend-specific attributes to a span
func AddBackendAttributes(span trace.Span, statusCode int, latency time.Duration, provider string) {
	span.SetAttributes(
		semconv.HTTPStatusCode(statusCode),
		attribute.Int64("kortex.latency_ms", latency.Milliseconds()),
	)
	if provider != "" {
		span.SetAttributes(attribute.String("kortex.provider", provider))
	}
}

// AddTokenAttributes adds token usage attributes to a span
func AddTokenAttributes(span trace.Span, inputTokens, outputTokens int) {
	span.SetAttributes(
		attribute.Int("kortex.tokens.input", inputTokens),
		attribute.Int("kortex.tokens.output", outputTokens),
		attribute.Int("kortex.tokens.total", inputTokens+outputTokens),
	)
}

// AddCostAttributes adds cost tracking attributes to a span
func AddCostAttributes(span trace.Span, cost float64, currency string) {
	span.SetAttributes(
		attribute.Float64("kortex.cost", cost),
		attribute.String("kortex.cost.currency", currency),
	)
}

// AddExperimentAttributes adds A/B experiment attributes to a span
func AddExperimentAttributes(span trace.Span, experimentName, variant string) {
	span.SetAttributes(
		attribute.String("kortex.experiment.name", experimentName),
		attribute.String("kortex.experiment.variant", variant),
	)
}

// AddFallbackAttributes adds fallback information to a span
func AddFallbackAttributes(span trace.Span, fromBackend, toBackend string, attempt int) {
	span.SetAttributes(
		attribute.String("kortex.fallback.from", fromBackend),
		attribute.String("kortex.fallback.to", toBackend),
		attribute.Int("kortex.fallback.attempt", attempt),
	)
}

// InjectContext injects trace context into outgoing HTTP request headers
func InjectContext(ctx context.Context, r *http.Request) {
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(r.Header))
}
