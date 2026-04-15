package collector

import "testing"

func TestParseTodoWriteInput(t *testing.T) {
	cases := []struct {
		name  string
		attrs map[string]any
		want  int
	}{
		{
			name:  "nil attrs",
			attrs: nil,
			want:  0,
		},
		{
			name:  "missing key",
			attrs: map[string]any{"claude_code.tool.name": "TodoWrite"},
			want:  0,
		},
		{
			name: "wrong type for input",
			attrs: map[string]any{
				"claude_code.tool.input": 42,
			},
			want: 0,
		},
		{
			name: "malformed JSON",
			attrs: map[string]any{
				"claude_code.tool.input": "{not json",
			},
			want: 0,
		},
		{
			name: "empty todos array",
			attrs: map[string]any{
				"claude_code.tool.input": `{"todos":[]}`,
			},
			want: 0,
		},
		{
			name: "missing todos key",
			attrs: map[string]any{
				"claude_code.tool.input": `{"other":"field"}`,
			},
			want: 0,
		},
		{
			name: "three todos mixed status",
			attrs: map[string]any{
				"claude_code.tool.input": `{"todos":[
					{"content":"build","status":"completed","activeForm":"Building"},
					{"content":"ship","status":"in_progress","activeForm":"Shipping"},
					{"content":"celebrate","status":"pending"}
				]}`,
			},
			want: 3,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseTodoWriteInput(tc.attrs)
			if len(got) != tc.want {
				t.Fatalf("parseTodoWriteInput length = %d, want %d", len(got), tc.want)
			}
		})
	}
}

func TestParseTodoWriteInput_PreservesFields(t *testing.T) {
	attrs := map[string]any{
		"claude_code.tool.input": `{"todos":[{"content":"run tests","status":"in_progress","activeForm":"Running tests"}]}`,
	}
	got := parseTodoWriteInput(attrs)
	if len(got) != 1 {
		t.Fatalf("want 1 todo, got %d", len(got))
	}
	if got[0].Content != "run tests" {
		t.Errorf("Content = %q", got[0].Content)
	}
	if got[0].Status != "in_progress" {
		t.Errorf("Status = %q", got[0].Status)
	}
	if got[0].ActiveForm != "Running tests" {
		t.Errorf("ActiveForm = %q", got[0].ActiveForm)
	}
}
