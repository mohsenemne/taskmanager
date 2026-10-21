package observability

import (
	"context"
	"fmt"
	"net"
	"net/url"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TracingInfo describes the service inside the traces it emits.
type TracingInfo struct {
	ServiceName string
	Version     string
	Environment string
	// Endpoint is an OTLP/HTTP collector address such as http://jaeger:4318.
	// Empty means spans are written to stdout, which keeps local runs free of
	// extra infrastructure.
	Endpoint string
}

// NewTracerProvider builds the provider, installs it globally and configures
// W3C trace-context propagation so an incoming traceparent is honoured.
func NewTracerProvider(ctx context.Context, info TracingInfo) (*sdktrace.TracerProvider, error) {
	exporter, err := newSpanExporter(ctx, info.Endpoint)
	if err != nil {
		return nil, err
	}

	provider := newTracerProvider(ctx, exporter, info)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return provider, nil
}

func newTracerProvider(_ context.Context, exporter sdktrace.SpanExporter, info TracingInfo) *sdktrace.TracerProvider {
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			"",
			attribute.String("service.name", info.ServiceName),
			attribute.String("service.version", info.Version),
			attribute.String("deployment.environment", info.Environment),
		),
	)
	if err != nil {
		res = resource.Default()
	}

	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		// Locally everything is sampled. A production deployment would dial
		// this down to a ratio, which is why the sampler is not hard-coded into
		// the middleware.
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(1))),
	)
}

func newSpanExporter(ctx context.Context, endpoint string) (sdktrace.SpanExporter, error) {
	if endpoint == "" {
		return stdouttrace.New()
	}

	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("invalid OTLP endpoint %q", endpoint)
	}

	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "4318")
	}

	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(host)}
	if u.Scheme != "https" {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	return otlptracehttp.New(ctx, opts...)
}
