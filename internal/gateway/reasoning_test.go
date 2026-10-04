package gateway

import (
	"testing"

	wire "zenflash-llm/internal/protocol"
)

func TestRequestedReasoningEffortProtocolFields(t *testing.T) {
	tests := []struct {
		name     string
		protocol wire.Protocol
		payload  map[string]any
		want     string
		param    string
	}{
		{"chat", wire.Chat, map[string]any{"reasoning_effort": "high"}, "high", "reasoning_effort"},
		{"responses", wire.Responses, map[string]any{"reasoning": map[string]any{"effort": "max"}}, "max", "reasoning.effort"},
		{"anthropic", wire.Anthropic, map[string]any{"output_config": map[string]any{"effort": "low"}}, "low", "output_config.effort"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, param, present, valid := requestedReasoningEffort(tt.protocol, tt.payload)
			if !present || !valid || got != tt.want || param != tt.param {
				t.Fatalf("requestedReasoningEffort() = (%q, %q, %v, %v), want (%q, %q, true, true)", got, param, present, valid, tt.want, tt.param)
			}
		})
	}
	if _, _, present, valid := requestedReasoningEffort(wire.Chat, map[string]any{"reasoning_effort": 4}); !present || valid {
		t.Fatal("non-string effort was not marked invalid")
	}
}
