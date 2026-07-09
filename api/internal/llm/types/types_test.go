package types_test

import (
	"testing"

	"github.com/jpgomesr/NeuralVault/internal/llm/types"
)

func TestUsage_CacheFieldsDefaultToZero(t *testing.T) {
	u := types.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8}
	if u.CacheReadTokens != 0 || u.CacheCreationTokens != 0 {
		t.Fatalf("expected zero-value cache fields, got %+v", u)
	}
}
