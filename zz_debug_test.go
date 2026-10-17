package didit

import "testing"

func TestDebugEsc(t *testing.T) {
	b, err := encodeJSONBody(SessionsCreateParams{WorkflowID: "w", Metadata: Some[any](map[string]any{"h": "<b>"})})
	t.Log(string(b), err)
}
