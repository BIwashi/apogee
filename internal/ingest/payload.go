// Package ingest is the apogee collector's hook-event ingestion layer. It
// owns the disler-compatible payload schema, the trace reconstructor, and the
// HTTP handler that accepts inbound POSTs.
package ingest

import (
	"encoding/json"
	"errors"
	"time"
)

// Hook event type constants. These mirror the canonical list in
// web/app/lib/event-types.ts. Treat the catalogue as closed; unknown event
// types are tolerated but only stored as raw log records.
const (
	HookSessionStart      = "SessionStart"
	HookSessionEnd        = "SessionEnd"
	HookUserPromptSubmit  = "UserPromptSubmit"
	HookPreToolUse        = "PreToolUse"
	HookPostToolUse       = "PostToolUse"
	HookPostToolUseFail   = "PostToolUseFailure"
	HookNotification      = "Notification"
	HookPermissionRequest = "PermissionRequest"
	HookSubagentStart     = "SubagentStart"
	HookSubagentStop      = "SubagentStop"
	HookPreCompact        = "PreCompact"
	HookStop              = "Stop"
)

// HookEvent is the disler-compatible payload accepted by POST /v1/events.
type HookEvent struct {
	SourceApp     string          `json:"source_app"`
	SessionID     string          `json:"session_id"`
	HookEventType string          `json:"hook_event_type"`
	Timestamp     int64           `json:"timestamp"` // ms since epoch
	Payload       json.RawMessage `json:"payload,omitempty"`

	// Optional top-level fields. disler flattens many of these out of the
	// inner payload so the collector can read them without re-parsing.
	ToolName              string          `json:"tool_name,omitempty"`
	ToolUseID             string          `json:"tool_use_id,omitempty"`
	Error                 string          `json:"error,omitempty"`
	IsInterrupt           bool            `json:"is_interrupt,omitempty"`
	PermissionSuggestions []string        `json:"permission_suggestions,omitempty"`
	AgentID               string          `json:"agent_id,omitempty"`
	AgentType             string          `json:"agent_type,omitempty"`
	AgentTranscriptPath   string          `json:"agent_transcript_path,omitempty"`
	StopHookActive        bool            `json:"stop_hook_active,omitempty"`
	NotificationType      string          `json:"notification_type,omitempty"`
	CustomInstructions    string          `json:"custom_instructions,omitempty"`
	Source                string          `json:"source,omitempty"`
	Reason                string          `json:"reason,omitempty"`
	Summary               string          `json:"summary,omitempty"`
	ModelName             string          `json:"model_name,omitempty"`
	Prompt                string          `json:"prompt,omitempty"`
	Chat                  json.RawMessage `json:"chat,omitempty"`
}

// DecodeHookEvents parses a request body that may be either a single
// HookEvent JSON object or an array of HookEvent values and returns the
// resulting slice. An empty body or an empty array both yield a non-nil
// zero-length slice without an error; the caller decides whether that is
// valid for its use case.
func DecodeHookEvents(body []byte) ([]HookEvent, error) {
	trimmed := trimLeadingWhitespace(body)
	if len(trimmed) == 0 {
		return []HookEvent{}, nil
	}
	if trimmed[0] == '[' {
		var out []HookEvent
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, err
		}
		if out == nil {
			out = []HookEvent{}
		}
		return out, nil
	}
	var ev HookEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, err
	}
	return []HookEvent{ev}, nil
}

func trimLeadingWhitespace(b []byte) []byte {
	for i, c := range b {
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			return b[i:]
		}
	}
	return nil
}

// Time returns the event timestamp as a time.Time. ms-since-epoch is the
// canonical wire format; we round-trip via UnixMilli.
func (e *HookEvent) Time() time.Time {
	return time.UnixMilli(e.Timestamp)
}

// Validate enforces the four required fields. Everything else is best-effort.
func (e *HookEvent) Validate() error {
	if e == nil {
		return errors.New("event: nil")
	}
	if e.SourceApp == "" {
		return errors.New("event: source_app is required")
	}
	if e.SessionID == "" {
		return errors.New("event: session_id is required")
	}
	if e.HookEventType == "" {
		return errors.New("event: hook_event_type is required")
	}
	if e.Timestamp <= 0 {
		return errors.New("event: timestamp must be positive ms-since-epoch")
	}
	return nil
}
