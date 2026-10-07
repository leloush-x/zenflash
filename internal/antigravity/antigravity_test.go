package antigravity

import (
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
