// Package telemetry wires the OpenTelemetry SDK into the apogee
// collector. Configuration is read from standard `OTEL_*` environment
// variables (and a small number of apogee-specific overrides), with a
// fallback to defaults that keep export disabled until an endpoint is
// supplied. The exporter is OTLP — gRPC by default, HTTP/protobuf when
// requested. Failures during export are logged at WARN and never crash
// the reconstructor.
package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv1270 "go.opentelemetry.io/otel/semconv/v1.27.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/BIwashi/apogee/internal/version"
	apogeesemconv "github.com/BIwashi/apogee/semconv"
)

// Protocol enumerates the OTLP exporter wire protocols apogee supports.
type Protocol string

const (
	ProtocolGRPC Protocol = "grpc"
	ProtocolHTTP Protocol = "http/protobuf"
)

// Config holds the resolved telemetry configuration. Env > TOML >
// defaults; the exact resolution happens in LoadConfigFromEnv (env) and
// MergeFileConfig (TOML), in that order.
type Config struct {
	// Enabled toggles OTLP export. The default is "endpoint presence"
	// — when an endpoint is set, export is on, unless explicitly
	// disabled via APOGEE_OTLP_ENABLED=false.
	Enabled bool
	// Endpoint is the OTLP target, e.g. "localhost:4317" (gRPC) or
	// "https://api.honeycomb.io:443" (HTTP).
	Endpoint string
	// Protocol selects the wire format. Empty defaults to grpc.
	Protocol Protocol
	// Insecure skips TLS verification on the OTLP transport.
	Insecure bool
	// Headers are sent on every export request (e.g. auth tokens).
	Headers map[string]string
	// ServiceName is the OTel resource service name, default "apogee".
	ServiceName string
	// ServiceVersion is the apogee build version baked into the
	// service.version resource attribute.
	ServiceVersion string
	// ServiceInstanceID disambiguates multiple apogee processes.
	ServiceInstanceID string
	// ResourceAttrs are extra static attributes added to the OTel
	// resource alongside service.* keys.
	ResourceAttrs map[string]string
	// SampleRatio is the head-based sampling ratio in [0, 1]. 0 disables
	// sampling outright; 1 records every span.
	SampleRatio float64
}

// IsEnabled reports whether the resolved config asks for OTLP export.
func (c Config) IsEnabled() bool {
	return c.Enabled && c.Endpoint != ""
}

// LoadConfigFromEnv resolves a Config from the standard OTEL_* env vars
// and apogee-specific overrides. The returned Config never errors —
// missing or malformed values fall back to defaults.
func LoadConfigFromEnv() Config {
	cfg := Config{
		Protocol:       ProtocolGRPC,
		ServiceName:    "apogee",
		SampleRatio:    1.0,
		Headers:        map[string]string{},
		ResourceAttrs:  map[string]string{},
		ServiceVersion: version.Version,
	}

	if v := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")); v != "" {
		cfg.Endpoint = v
		cfg.Enabled = true
	}
	if v := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")); v != "" {
		cfg.Protocol = normaliseProtocol(v)
	}
	if v := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_INSECURE")); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Insecure = b
		}
	}
	if v := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")); v != "" {
		for k, val := range parseKVList(v) {
			cfg.Headers[k] = val
		}
	}
	if v := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME")); v != "" {
		cfg.ServiceName = v
	}
	if v := strings.TrimSpace(os.Getenv("OTEL_RESOURCE_ATTRIBUTES")); v != "" {
		for k, val := range parseKVList(v) {
			cfg.ResourceAttrs[k] = val
		}
	}
	if v := strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER_ARG")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			cfg.SampleRatio = clamp01(f)
		}
	}
	if v := strings.TrimSpace(os.Getenv("APOGEE_OTLP_ENABLED")); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Enabled = b
		}
	}
	cfg.ServiceInstanceID = resolveInstanceID()
	return cfg
}

// resolveInstanceID returns a stable per-process id. We prefer the
// hostname for readability and fall back to the PID when hostname is
// unavailable.
func resolveInstanceID() string {
	h, err := os.Hostname()
	if err == nil && h != "" {
		return fmt.Sprintf("%s-%d", h, os.Getpid())
	}
	return fmt.Sprintf("apogee-%d", os.Getpid())
}

func normaliseProtocol(in string) Protocol {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "grpc":
		return ProtocolGRPC
	case "http", "http/protobuf", "http/proto":
		return ProtocolHTTP
	default:
		return ProtocolGRPC
	}
}

func parseKVList(in string) map[string]string {
	out := map[string]string{}
	if in == "" {
		return out
	}
	for _, raw := range strings.Split(in, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		eq := strings.IndexByte(raw, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(raw[:eq])
		val := strings.TrimSpace(raw[eq+1:])
		if key == "" {
			continue
		}
		out[key] = val
	}
	return out
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// CountingExporter wraps a sdktrace.SpanExporter and increments an
// atomic counter on every successful batch. The collector exposes the
// counter via /v1/telemetry/status.
type CountingExporter struct {
	inner   sdktrace.SpanExporter
	counter *atomic.Uint64
	logger  *slog.Logger
}

// NewCountingExporter wraps an inner exporter. counter must be non-nil.
func NewCountingExporter(inner sdktrace.SpanExporter, counter *atomic.Uint64, logger *slog.Logger) *CountingExporter {
	return &CountingExporter{inner: inner, counter: counter, logger: logger}
}

// ExportSpans forwards to the inner exporter. The counter is bumped
// regardless of success so operators can see batches flowing even
// when the remote backend is misconfigured. Errors are logged at WARN
// — the reconstructor never blocks on export failures.
func (e *CountingExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if e == nil || e.inner == nil {
		return nil
	}
	if e.counter != nil {
		e.counter.Add(uint64(len(spans)))
	}
	err := e.inner.ExportSpans(ctx, spans)
	if err != nil && e.logger != nil {
		e.logger.Warn("otel: export spans failed", "err", err, "batch", len(spans))
	}
	return err
}

// Shutdown forwards to the inner exporter.
func (e *CountingExporter) Shutdown(ctx context.Context) error {
	if e == nil || e.inner == nil {
		return nil
	}
	return e.inner.Shutdown(ctx)
}

// Provider bundles a TracerProvider together with its export counter
// and a shutdown callback. The collector stores one of these on the
// Server struct so HTTP handlers can read the counter and so Run can
// flush spans on shutdown.
type Provider struct {
	TP            trace.TracerProvider
	Shutdown      func(context.Context) error
	SpansExported *atomic.Uint64
	Cfg           Config
	Enabled       bool
}

// Tracer returns a tracer suitable for the apogee reconstructor. Always
// returns a usable Tracer — when the provider is disabled, the noop
// implementation is returned so callers don't need nil checks.
func (p *Provider) Tracer() trace.Tracer {
	if p == nil || p.TP == nil {
		return tracenoop.NewTracerProvider().Tracer("apogee/reconstructor")
	}
	return p.TP.Tracer("apogee/reconstructor")
}

// NewTracerProvider builds a TracerProvider from cfg. When cfg is
// disabled the returned provider is a noop TracerProvider — the
// reconstructor can still call Tracer.Start safely. The shutdown
// callback flushes the exporter and never errors when disabled.
//
// On any construction error, the noop provider is returned and the
// error is logged. Telemetry is best-effort; misconfiguration must
// never block the collector.
func NewTracerProvider(ctx context.Context, cfg Config, logger *slog.Logger) (*Provider, error) {
	if logger == nil {
		logger = slog.Default()
	}
	counter := &atomic.Uint64{}

	if !cfg.IsEnabled() {
		logger.Info("otel: export disabled — using noop tracer provider")
		noopTP := tracenoop.NewTracerProvider()
		return &Provider{
			TP:            noopTP,
			Shutdown:      func(context.Context) error { return nil },
			SpansExported: counter,
			Cfg:           cfg,
			Enabled:       false,
		}, nil
	}

	exporter, err := buildExporter(ctx, cfg)
	if err != nil {
		logger.Warn("otel: build exporter failed — falling back to noop", "err", err)
		noopTP := tracenoop.NewTracerProvider()
		return &Provider{
			TP:            noopTP,
			Shutdown:      func(context.Context) error { return nil },
			SpansExported: counter,
			Cfg:           cfg,
			Enabled:       false,
		}, nil
	}

	wrapped := NewCountingExporter(exporter, counter, logger)

	res, err := buildResource(ctx, cfg)
	if err != nil {
		logger.Warn("otel: build resource failed — using empty resource", "err", err)
		res = resource.Empty()
	}

	bsp := sdktrace.NewBatchSpanProcessor(wrapped,
		sdktrace.WithBatchTimeout(2*time.Second),
		sdktrace.WithMaxExportBatchSize(512),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
	)
	otel.SetTracerProvider(tp)

	logger.Info("otel: tracer provider ready",
		"endpoint", cfg.Endpoint,
		"protocol", string(cfg.Protocol),
		"service", cfg.ServiceName,
		"sample_ratio", cfg.SampleRatio,
	)

	shutdown := func(ctx context.Context) error {
		// Best-effort flush, then orderly shutdown.
		flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := tp.ForceFlush(flushCtx); err != nil {
			logger.Warn("otel: flush failed", "err", err)
		}
		return tp.Shutdown(ctx)
	}
	return &Provider{
		TP:            tp,
		Shutdown:      shutdown,
		SpansExported: counter,
		Cfg:           cfg,
		Enabled:       true,
	}, nil
}

func buildExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	switch cfg.Protocol {
	case ProtocolHTTP:
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(stripScheme(cfg.Endpoint)),
		}
		if cfg.Insecure || strings.HasPrefix(cfg.Endpoint, "http://") {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(cfg.Headers))
		}
		return otlptrace.New(ctx, otlptracehttp.NewClient(opts...))
	default:
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(stripScheme(cfg.Endpoint)),
		}
		if cfg.Insecure || strings.HasPrefix(cfg.Endpoint, "http://") {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(cfg.Headers))
		}
		return otlptrace.New(ctx, otlptracegrpc.NewClient(opts...))
	}
}

func stripScheme(endpoint string) string {
	for _, prefix := range []string{"https://", "http://"} {
		if strings.HasPrefix(endpoint, prefix) {
			return strings.TrimPrefix(endpoint, prefix)
		}
	}
	return endpoint
}

func buildResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv1270.ServiceName(cfg.ServiceName),
		semconv1270.ServiceVersion(cfg.ServiceVersion),
		semconv1270.ServiceInstanceID(cfg.ServiceInstanceID),
		apogeesemconv.SessionSourceApp.String(cfg.ServiceName),
	}
	for k, v := range cfg.ResourceAttrs {
		attrs = append(attrs, attribute.String(k, v))
	}
	// We deliberately do not pin a SchemaURL on the resource because
	// resource.WithProcess/WithHost auto-detectors may declare a
	// different schema version, which would make resource.New error
	// out with "conflicting Schema URL". Dropping the schema lets the
	// SDK merge the auto-detected attributes cleanly. Per-attribute
	// schema versioning is preserved by the underlying semconv
	// constants.
	return resource.New(ctx,
		resource.WithAttributes(attrs...),
		resource.WithProcess(),
		resource.WithHost(),
		resource.WithFromEnv(),
	)
}
