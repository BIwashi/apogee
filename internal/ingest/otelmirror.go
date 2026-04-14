package ingest

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/BIwashi/apogee/internal/otel"
	apogeesemconv "github.com/BIwashi/apogee/semconv"
)

// otelMirrorEnabled reports whether the reconstructor is configured to
// emit OpenTelemetry spans alongside its DuckDB writes.
func (r *Reconstructor) otelMirrorEnabled() bool {
	return r != nil && r.Tracer != nil
}

// startOTelSpan creates an OTel-side mirror for the apogee span sp. The
// returned context is parented at parentCtx (or context.Background when
// parentCtx is nil), and the returned span carries the same name as the
// apogee span. On success the apogee TraceID and SpanID are replaced
// with the OTel-generated values so the persisted ids match the trace
// ids that downstream OTLP backends will see.
//
// The function never errors; if the tracer is nil or the OTel SDK is
// otherwise unhappy the returned context is parentCtx and the returned
// span is nil. Callers must guard for nil before calling End or
// SetAttributes.
func (r *Reconstructor) startOTelSpan(parentCtx context.Context, sp *otel.Span, kind oteltrace.SpanKind) (context.Context, oteltrace.Span) {
	if !r.otelMirrorEnabled() || sp == nil {
		return parentCtx, nil
	}
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	opts := []oteltrace.SpanStartOption{
		oteltrace.WithSpanKind(kind),
		oteltrace.WithTimestamp(sp.StartTime),
	}
	ctx, otSpan := r.Tracer.Start(parentCtx, sp.Name, opts...)
	if otSpan == nil {
		return parentCtx, nil
	}
	sc := otSpan.SpanContext()
	if sc.HasTraceID() {
		sp.TraceID = otel.TraceID(sc.TraceID().String())
	}
	if sc.HasSpanID() {
		sp.SpanID = otel.SpanID(sc.SpanID().String())
	}
	if parent := oteltrace.SpanFromContext(parentCtx); parent != nil {
		if psc := parent.SpanContext(); psc.HasSpanID() {
			sp.ParentSpanID = otel.SpanID(psc.SpanID().String())
		}
	}
	r.applyOTelAttributes(otSpan, sp)
	r.applyOTelEvents(otSpan, sp)
	return ctx, otSpan
}

// finishOTelSpan stamps the final attributes, status and end time on
// the mirror span. Safe to call with a nil otSpan.
func (r *Reconstructor) finishOTelSpan(otSpan oteltrace.Span, sp *otel.Span) {
	if otSpan == nil || sp == nil {
		return
	}
	r.applyOTelAttributes(otSpan, sp)
	r.applyOTelEvents(otSpan, sp)
	switch sp.StatusCode {
	case otel.StatusOK:
		otSpan.SetStatus(codes.Ok, sp.StatusMessage)
	case otel.StatusError:
		otSpan.SetStatus(codes.Error, sp.StatusMessage)
	}
	if sp.EndTime != nil {
		otSpan.End(oteltrace.WithTimestamp(*sp.EndTime))
	} else {
		otSpan.End()
	}
}

// updateOTelSpan refreshes attributes/events on a still-open mirror
// span. Used when the apogee side appends a span event without
// finishing the span (e.g. notifications, compaction markers).
func (r *Reconstructor) updateOTelSpan(otSpan oteltrace.Span, sp *otel.Span) {
	if otSpan == nil || sp == nil {
		return
	}
	r.applyOTelAttributes(otSpan, sp)
	r.applyOTelEvents(otSpan, sp)
}

// applyOTelAttributes maps the apogee in-memory attribute bag to OTel
// KeyValues using the constants from `semconv/attrs.go`. Unknown keys
// are forwarded verbatim so callers can experiment without editing
// the registry.
func (r *Reconstructor) applyOTelAttributes(otSpan oteltrace.Span, sp *otel.Span) {
	if otSpan == nil || sp == nil {
		return
	}
	kvs := make([]attribute.KeyValue, 0, len(sp.Attributes)+8)
	if sp.SessionID != "" {
		kvs = append(kvs, apogeesemconv.SessionID.String(sp.SessionID))
	}
	if sp.TurnID != "" {
		kvs = append(kvs, apogeesemconv.TurnID.String(sp.TurnID))
	}
	if sp.AgentID != "" {
		kvs = append(kvs, apogeesemconv.AgentID.String(sp.AgentID))
	}
	if sp.AgentKind != "" {
		kvs = append(kvs, apogeesemconv.AgentKind.String(sp.AgentKind))
	}
	if sp.ToolName != "" {
		kvs = append(kvs, apogeesemconv.ToolName.String(sp.ToolName))
	}
	if sp.ToolUseID != "" {
		kvs = append(kvs, apogeesemconv.ToolUseID.String(sp.ToolUseID))
	}
	if sp.MCPServer != "" {
		kvs = append(kvs, apogeesemconv.ToolMCPServer.String(sp.MCPServer))
	}
	if sp.MCPTool != "" {
		kvs = append(kvs, apogeesemconv.ToolMCPName.String(sp.MCPTool))
	}
	for k, v := range sp.Attributes {
		if kv, ok := mapAttribute(k, v); ok {
			kvs = append(kvs, kv)
		}
	}
	if len(kvs) > 0 {
		otSpan.SetAttributes(kvs...)
	}
}

// applyOTelEvents mirrors the apogee SpanEvent slice onto the OTel
// span. We always re-add every event; OTel's event semantics tolerate
// duplicates and we do not currently track which events have already
// been forwarded. This is a side channel — exact event dedup is not
// required for trace export to be useful.
func (r *Reconstructor) applyOTelEvents(otSpan oteltrace.Span, sp *otel.Span) {
	if otSpan == nil || sp == nil || len(sp.Events) == 0 {
		return
	}
	for _, ev := range sp.Events {
		var kvs []attribute.KeyValue
		for k, v := range ev.Attributes {
			if kv, ok := mapAttribute(k, v); ok {
				kvs = append(kvs, kv)
			}
		}
		opts := []oteltrace.EventOption{}
		if !ev.Time.IsZero() {
			opts = append(opts, oteltrace.WithTimestamp(ev.Time))
		}
		if len(kvs) > 0 {
			opts = append(opts, oteltrace.WithAttributes(kvs...))
		}
		otSpan.AddEvent(ev.Name, opts...)
	}
}

// mapAttribute coerces a free-form interface value into an
// attribute.KeyValue using the OTel attribute primitives. Unsupported
// shapes return ok=false and are silently dropped. We deliberately
// keep this conservative — the goal is "correct or absent", not
// exhaustive coverage of every possible Go shape.
func mapAttribute(key string, val any) (attribute.KeyValue, bool) {
	if key == "" {
		return attribute.KeyValue{}, false
	}
	k := attribute.Key(key)
	switch v := val.(type) {
	case nil:
		return attribute.KeyValue{}, false
	case string:
		return k.String(v), true
	case bool:
		return k.Bool(v), true
	case int:
		return k.Int64(int64(v)), true
	case int32:
		return k.Int64(int64(v)), true
	case int64:
		return k.Int64(v), true
	case float32:
		return k.Float64(float64(v)), true
	case float64:
		return k.Float64(v), true
	case []string:
		return k.StringSlice(v), true
	default:
		// Fall back to fmt.Sprintf via attribute.Stringer would be
		// noisier than helpful here. Skip unknown shapes.
		return attribute.KeyValue{}, false
	}
}

// EmitRecapEnrichment fires a fresh "claude_code.turn.recap" span that
// carries recap attributes for the given turn. The span is parented at
// a remote span context built from the turn root id pair, mirroring
// the OTel "post-hoc enrichment" pattern. Safe to call from any
// goroutine — the OTel SDK handles its own synchronisation. No-op
// when the tracer is nil.
func (r *Reconstructor) EmitRecapEnrichment(ctx context.Context, traceIDHex, spanIDHex string, attrs []attribute.KeyValue, startedAt, endedAt int64) {
	if !r.otelMirrorEnabled() {
		return
	}
	tid, err := oteltrace.TraceIDFromHex(traceIDHex)
	if err != nil {
		return
	}
	sid, err := oteltrace.SpanIDFromHex(spanIDHex)
	if err != nil {
		return
	}
	parent := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    tid,
		SpanID:     sid,
		TraceFlags: oteltrace.FlagsSampled,
		Remote:     true,
	})
	if ctx == nil {
		ctx = context.Background()
	}
	parentCtx := oteltrace.ContextWithRemoteSpanContext(ctx, parent)
	_, span := r.Tracer.Start(parentCtx, apogeesemconv.SpanTurnRecap,
		oteltrace.WithSpanKind(oteltrace.SpanKindInternal),
		oteltrace.WithLinks(oteltrace.Link{SpanContext: parent}),
	)
	if span == nil {
		return
	}
	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	span.End()
}
