package telemetry

import (
	"testing"
)

func clearOTelEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"OTEL_EXPORTER_OTLP_INSECURE",
		"OTEL_EXPORTER_OTLP_HEADERS",
		"OTEL_SERVICE_NAME",
		"OTEL_RESOURCE_ATTRIBUTES",
		"OTEL_TRACES_SAMPLER_ARG",
		"APOGEE_OTLP_ENABLED",
	} {
		t.Setenv(k, "")
	}
}

func TestLoadConfigFromEnv_Defaults(t *testing.T) {
	clearOTelEnv(t)
	cfg := LoadConfigFromEnv()
	if cfg.Enabled {
		t.Fatal("expected default config to be disabled")
	}
	if cfg.IsEnabled() {
		t.Fatal("IsEnabled must be false when no endpoint is configured")
	}
	if cfg.ServiceName != "apogee" {
		t.Fatalf("default service name = %q, want apogee", cfg.ServiceName)
	}
	if cfg.Protocol != ProtocolGRPC {
		t.Fatalf("default protocol = %q, want %q", cfg.Protocol, ProtocolGRPC)
	}
	if cfg.SampleRatio != 1.0 {
		t.Fatalf("default sample ratio = %v, want 1.0", cfg.SampleRatio)
	}
}

func TestLoadConfigFromEnv_EndpointEnables(t *testing.T) {
	clearOTelEnv(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4317")
	cfg := LoadConfigFromEnv()
	if !cfg.Enabled {
		t.Fatal("endpoint should enable export")
	}
	if !cfg.IsEnabled() {
		t.Fatal("IsEnabled must be true once an endpoint is set")
	}
	if cfg.Endpoint != "http://localhost:4317" {
		t.Fatalf("endpoint = %q, want http://localhost:4317", cfg.Endpoint)
	}
}

func TestLoadConfigFromEnv_ApogeeForceDisable(t *testing.T) {
	clearOTelEnv(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4317")
	t.Setenv("APOGEE_OTLP_ENABLED", "false")
	cfg := LoadConfigFromEnv()
	if cfg.Enabled {
		t.Fatal("APOGEE_OTLP_ENABLED=false must override endpoint presence")
	}
}

func TestLoadConfigFromEnv_ProtocolAndHeaders(t *testing.T) {
	clearOTelEnv(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://api.example.com:443")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "x-token=abc , x-team = xyz")
	t.Setenv("OTEL_SERVICE_NAME", "apogee-test")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment=ci,cluster=local")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.25")
	cfg := LoadConfigFromEnv()
	if cfg.Protocol != ProtocolHTTP {
		t.Fatalf("protocol = %q, want %q", cfg.Protocol, ProtocolHTTP)
	}
	if got := cfg.Headers["x-token"]; got != "abc" {
		t.Fatalf("header x-token = %q, want abc", got)
	}
	if got := cfg.Headers["x-team"]; got != "xyz" {
		t.Fatalf("header x-team = %q, want xyz", got)
	}
	if cfg.ServiceName != "apogee-test" {
		t.Fatalf("service name = %q, want apogee-test", cfg.ServiceName)
	}
	if cfg.ResourceAttrs["deployment.environment"] != "ci" {
		t.Fatalf("resource attr missing: %v", cfg.ResourceAttrs)
	}
	if cfg.SampleRatio != 0.25 {
		t.Fatalf("sample ratio = %v, want 0.25", cfg.SampleRatio)
	}
}

func TestNewTracerProvider_Disabled(t *testing.T) {
	clearOTelEnv(t)
	cfg := LoadConfigFromEnv()
	prov, err := NewTracerProvider(t.Context(), cfg, nil)
	if err != nil {
		t.Fatalf("NewTracerProvider: %v", err)
	}
	if prov == nil {
		t.Fatal("expected non-nil provider")
	}
	if prov.Enabled {
		t.Fatal("provider should not be marked enabled when disabled")
	}
	if prov.Tracer() == nil {
		t.Fatal("Tracer must always return a non-nil tracer (noop fallback)")
	}
	if prov.SpansExported == nil {
		t.Fatal("SpansExported counter must be present")
	}
	if err := prov.Shutdown(t.Context()); err != nil {
		t.Fatalf("noop shutdown: %v", err)
	}
}

func TestNewTracerProvider_BogusEndpointStillStarts(t *testing.T) {
	clearOTelEnv(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1") // unreachable
	cfg := LoadConfigFromEnv()
	prov, err := NewTracerProvider(t.Context(), cfg, nil)
	if err != nil {
		t.Fatalf("NewTracerProvider: %v", err)
	}
	if prov == nil || prov.TP == nil {
		t.Fatal("provider must be constructed even with an unreachable endpoint")
	}
	// Force a no-op tracer call to ensure the shape is wired.
	tr := prov.Tracer()
	_, span := tr.Start(t.Context(), "smoke")
	span.End()
	_ = prov.Shutdown(t.Context())
}

func TestParseKVList(t *testing.T) {
	got := parseKVList("a=1, b = 2,c =, =bad,,d=four ")
	want := map[string]string{"a": "1", "b": "2", "c": "", "d": "four"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %q = %q, want %q", k, got[k], v)
		}
	}
}

func TestNormaliseProtocol(t *testing.T) {
	cases := map[string]Protocol{
		"":              ProtocolGRPC,
		"grpc":          ProtocolGRPC,
		"GRPC":          ProtocolGRPC,
		"http":          ProtocolHTTP,
		"http/protobuf": ProtocolHTTP,
		"weird":         ProtocolGRPC,
	}
	for in, want := range cases {
		if got := normaliseProtocol(in); got != want {
			t.Errorf("normaliseProtocol(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripScheme(t *testing.T) {
	cases := map[string]string{
		"localhost:4317":          "localhost:4317",
		"http://localhost:4318":   "localhost:4318",
		"https://api.example.com": "api.example.com",
	}
	for in, want := range cases {
		if got := stripScheme(in); got != want {
			t.Errorf("stripScheme(%q) = %q, want %q", in, got, want)
		}
	}
}
