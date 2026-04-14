package ingest

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHookEventUnmarshal(t *testing.T) {
	src := `{
		"source_app": "demo",
		"session_id": "s-1",
		"hook_event_type": "PreToolUse",
		"timestamp": 1700000000000,
		"tool_name": "Bash",
		"tool_use_id": "tu-1",
		"payload": {"command": "ls"}
	}`
	var ev HookEvent
	require.NoError(t, json.Unmarshal([]byte(src), &ev))
	require.NoError(t, ev.Validate())
	require.Equal(t, "demo", ev.SourceApp)
	require.Equal(t, "PreToolUse", ev.HookEventType)
	require.Equal(t, "Bash", ev.ToolName)
	require.Equal(t, "tu-1", ev.ToolUseID)
	require.Equal(t, time.UnixMilli(1700000000000), ev.Time())
}

func TestDecodeHookEvents(t *testing.T) {
	single := `{"source_app":"demo","session_id":"s","hook_event_type":"Stop","timestamp":1}`
	batch := `[
		{"source_app":"demo","session_id":"s","hook_event_type":"Stop","timestamp":1},
		{"source_app":"demo","session_id":"s","hook_event_type":"SessionEnd","timestamp":2}
	]`

	t.Run("single object", func(t *testing.T) {
		out, err := DecodeHookEvents([]byte(single))
		require.NoError(t, err)
		require.Len(t, out, 1)
		require.Equal(t, "Stop", out[0].HookEventType)
	})

	t.Run("json array", func(t *testing.T) {
		out, err := DecodeHookEvents([]byte(batch))
		require.NoError(t, err)
		require.Len(t, out, 2)
		require.Equal(t, "Stop", out[0].HookEventType)
		require.Equal(t, "SessionEnd", out[1].HookEventType)
	})

	t.Run("leading whitespace array", func(t *testing.T) {
		out, err := DecodeHookEvents([]byte("  \n" + batch))
		require.NoError(t, err)
		require.Len(t, out, 2)
	})

	t.Run("empty body", func(t *testing.T) {
		out, err := DecodeHookEvents(nil)
		require.NoError(t, err)
		require.Len(t, out, 0)
	})

	t.Run("empty array", func(t *testing.T) {
		out, err := DecodeHookEvents([]byte("[]"))
		require.NoError(t, err)
		require.NotNil(t, out)
		require.Len(t, out, 0)
	})

	t.Run("invalid json", func(t *testing.T) {
		_, err := DecodeHookEvents([]byte("{not json"))
		require.Error(t, err)
	})
}

func TestHookEventValidate(t *testing.T) {
	cases := []struct {
		name    string
		event   HookEvent
		wantErr bool
	}{
		{"missing source_app", HookEvent{SessionID: "s", HookEventType: "Stop", Timestamp: 1}, true},
		{"missing session", HookEvent{SourceApp: "x", HookEventType: "Stop", Timestamp: 1}, true},
		{"missing event type", HookEvent{SourceApp: "x", SessionID: "s", Timestamp: 1}, true},
		{"missing timestamp", HookEvent{SourceApp: "x", SessionID: "s", HookEventType: "Stop"}, true},
		{"valid", HookEvent{SourceApp: "x", SessionID: "s", HookEventType: "Stop", Timestamp: 1}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.event.Validate()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
