package otel

import (
	"encoding/json"
	"time"
)

// StatusCode mirrors the OpenTelemetry status code enum.
type StatusCode string

const (
	StatusUnset StatusCode = "UNSET"
	StatusOK    StatusCode = "OK"
	StatusError StatusCode = "ERROR"
)

// SpanKind mirrors the OpenTelemetry span kind enum.
type SpanKind string

const (
	SpanKindInternal SpanKind = "INTERNAL"
	SpanKindServer   SpanKind = "SERVER"
	SpanKindClient   SpanKind = "CLIENT"
)

// Span is the in-process representation of an OpenTelemetry span. It mirrors
// the columns of the spans table in DuckDB without coupling either side to a
// specific SDK release.
type Span struct {
	TraceID       TraceID
	SpanID        SpanID
	ParentSpanID  SpanID
	Name          string
	Kind          SpanKind
	StartTime     time.Time
	EndTime       *time.Time
	StatusCode    StatusCode
	StatusMessage string
	ServiceName   string

	// Denormalised hot attributes the dashboard filters on.
	SessionID  string
	TurnID     string
	AgentID    string
	AgentKind  string
	ToolName   string
	ToolUseID  string
	MCPServer  string
	MCPTool    string
	HookEvent  string
	Attributes map[string]any
	Events     []SpanEvent
}

// SpanEvent is a timestamped annotation attached to a span. It maps to OTel
// span events.
type SpanEvent struct {
	Name       string         `json:"name"`
	Time       time.Time      `json:"time"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// DurationNanos returns the span duration, or zero when the span is still
// open.
func (s *Span) DurationNanos() int64 {
	if s.EndTime == nil {
		return 0
	}
	return s.EndTime.Sub(s.StartTime).Nanoseconds()
}

// AttributesJSON serialises the attribute bag, defaulting to "{}" so the
// stored value is always valid JSON.
func (s *Span) AttributesJSON() string {
	if len(s.Attributes) == 0 {
		return "{}"
	}
	b, err := json.Marshal(s.Attributes)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// EventsJSON serialises the events slice, defaulting to "[]".
func (s *Span) EventsJSON() string {
	if len(s.Events) == 0 {
		return "[]"
	}
	b, err := json.Marshal(s.Events)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// LogRecord is the in-process representation of an OpenTelemetry log record.
type LogRecord struct {
	Timestamp      time.Time
	TraceID        TraceID
	SpanID         SpanID
	SeverityText   string
	SeverityNumber int
	Body           string
	SessionID      string
	TurnID         string
	HookEvent      string
	SourceApp      string
	Attributes     map[string]any
}

// AttributesJSON serialises the log attribute bag.
func (l *LogRecord) AttributesJSON() string {
	if len(l.Attributes) == 0 {
		return "{}"
	}
	b, err := json.Marshal(l.Attributes)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// MetricKind enumerates the metric value shapes apogee currently records.
type MetricKind string

const (
	MetricCounter   MetricKind = "counter"
	MetricGauge     MetricKind = "gauge"
	MetricHistogram MetricKind = "histogram"
)

// MetricPoint is the in-process representation of a single OTel metric data
// point. apogee writes these into the metric_points table; PR #8 will export
// them via OTLP.
type MetricPoint struct {
	Timestamp time.Time
	Name      string
	Kind      MetricKind
	Value     float64
	Histogram *Histogram
	Unit      string
	Labels    map[string]string
}

// Histogram is the explicit-bucket histogram body for a MetricPoint.
type Histogram struct {
	Count          uint64    `json:"count"`
	Sum            float64   `json:"sum"`
	BucketCounts   []uint64  `json:"bucket_counts"`
	ExplicitBounds []float64 `json:"explicit_bounds"`
}
