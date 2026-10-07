package antigravity

import (
	"io"
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeGeminiSchemaDropsRejectedFields(t *testing.T) {
	raw := `{"type":"object","$schema":"http://json-schema.org/draft-07/schema#","properties":{"count":{"anyOf":[{"type":"integer","minimum":1,"exclusiveMinimum":true}],"description":"n"}},"required":["count"]}`
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatal(err)
	}
	cleaned, _ := json.Marshal(sanitizeGeminiSchema(v))
	s := string(cleaned)
	for _, banned := range []string{"$schema", "exclusiveMinimum", "anyOf"} {
		if strings.Contains(s, banned) {
			t.Fatalf("sanitized schema still contains %q: %s", banned, s)
		}
	}
	for _, want := range []string{`"type":"integer"`, `"minimum":1`, `"required":["count"]`} {
		if !strings.Contains(s, want) {
			t.Fatalf("sanitized schema lost %q: %s", want, s)
		}
	}
}

func TestBuildGenerateRequestSanitizesToolParameters(t *testing.T) {
	chat := `{"model":"gemini-3.6-flash-medium","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"read","description":"read a file","parameters":{"type":"object","$schema":"x","properties":{"path":{"type":"string"}},"required":["path"]}}}]}`
	env, _, err := BuildGenerateRequest([]byte(chat), "gemini-3.6-flash-medium", "proj")
	if err != nil {
		t.Fatal(err)
	}
	s := string(env)
	if strings.Contains(s, "$schema") {
		t.Fatalf("request still carries $schema: %s", s)
	}
	if !strings.Contains(s, "functionDeclarations") {
		t.Fatalf("request lost tool declarations: %s", s)
	}
}

func TestToStreamResponseEmitsDeltas(t *testing.T) {
	doc := `{"id":"antigravity-1","object":"chat.completion","created":1,"model":"claude-sonnet-4-6","choices":[{"index":0,"message":{"role":"assistant","content":"hi there"},"finish_reason":"stop"}],"usage":{"prompt_tokens":8}}`
	resp := ToStreamResponse([]byte(doc))
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, `"object":"chat.completion.chunk"`) {
		t.Fatalf("stream must use chunk objects: %s", s)
	}
	if !strings.Contains(s, `"delta":{"content":"hi there"}`) {
		t.Fatalf("stream must carry a content delta: %s", s)
	}
	if !strings.Contains(s, "data: [DONE]") {
		t.Fatalf("stream must terminate: %s", s)
	}
}
