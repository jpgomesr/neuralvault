package catalog

import "testing"

func TestLookup(t *testing.T) {
	tests := []struct {
		name           string
		provider       Provider
		wantOK         bool
		wantKind       Kind
		wantRequiresAK bool
	}{
		{name: "ollama", provider: Ollama, wantOK: true, wantKind: KindOllama, wantRequiresAK: false},
		{name: "anthropic", provider: Anthropic, wantOK: true, wantKind: KindAnthropic, wantRequiresAK: true},
		{name: "gemini", provider: Gemini, wantOK: true, wantKind: KindOpenAICompat, wantRequiresAK: true},
		{name: "openrouter", provider: OpenRouter, wantOK: true, wantKind: KindOpenAICompat, wantRequiresAK: true},
		{name: "groq", provider: Groq, wantOK: true, wantKind: KindOpenAICompat, wantRequiresAK: true},
		{name: "github", provider: GitHub, wantOK: true, wantKind: KindOpenAICompat, wantRequiresAK: true},
		{name: "openai", provider: OpenAI, wantOK: true, wantKind: KindOpenAICompat, wantRequiresAK: true},
		{name: "unknown provider", provider: Provider("does-not-exist"), wantOK: false},
		{name: "empty provider", provider: Provider(""), wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := Lookup(tt.provider)
			if ok != tt.wantOK {
				t.Fatalf("Lookup(%q) ok = %v, want %v", tt.provider, ok, tt.wantOK)
			}
			if !tt.wantOK {
				if entry != (Entry{}) {
					t.Fatalf("Lookup(%q) = %+v, want zero Entry for an unknown provider", tt.provider, entry)
				}
				return
			}
			if entry.Provider != tt.provider {
				t.Errorf("Provider = %q, want %q", entry.Provider, tt.provider)
			}
			if entry.Kind != tt.wantKind {
				t.Errorf("Kind = %q, want %q", entry.Kind, tt.wantKind)
			}
			if entry.RequiresAPIKey != tt.wantRequiresAK {
				t.Errorf("RequiresAPIKey = %v, want %v", entry.RequiresAPIKey, tt.wantRequiresAK)
			}
			if entry.Name == "" {
				t.Error("Name is empty")
			}
		})
	}
}

// TestLookup_OllamaHasNoBaseURL guards the invariant resolver.credentialFor
// relies on: Ollama's endpoint comes from server config, not the catalog.
func TestLookup_OllamaHasNoBaseURL(t *testing.T) {
	entry, ok := Lookup(Ollama)
	if !ok {
		t.Fatal("Lookup(Ollama) not found")
	}
	if entry.BaseURL != "" {
		t.Errorf("Ollama BaseURL = %q, want empty", entry.BaseURL)
	}
}

// TestLookup_KeyedProvidersHaveBaseURL guards the invariant every other
// provider relies on a hardcoded default endpoint.
func TestLookup_KeyedProvidersHaveBaseURL(t *testing.T) {
	for _, e := range All() {
		if e.Provider == Ollama {
			continue
		}
		if e.BaseURL == "" {
			t.Errorf("provider %q has RequiresAPIKey=%v but an empty BaseURL", e.Provider, e.RequiresAPIKey)
		}
	}
}

// TestLookup_AnthropicHasNoEmbeddings guards the invariant embedderFor relies
// on: selecting Anthropic as an embedding provider must be rejected upstream,
// since it has no embeddings API at all.
func TestLookup_AnthropicHasNoEmbeddings(t *testing.T) {
	entry, ok := Lookup(Anthropic)
	if !ok {
		t.Fatal("Lookup(Anthropic) not found")
	}
	if entry.SupportsEmbeddings {
		t.Error("Anthropic SupportsEmbeddings = true, want false")
	}
}

func TestAll(t *testing.T) {
	all := All()

	if len(all) != len(entries) {
		t.Fatalf("len(All()) = %d, want %d", len(all), len(entries))
	}
	if all[0].Provider != Ollama {
		t.Errorf("All()[0].Provider = %q, want %q (the local default must be listed first)", all[0].Provider, Ollama)
	}

	seen := make(map[Provider]bool, len(all))
	for _, e := range all {
		if seen[e.Provider] {
			t.Errorf("provider %q listed more than once", e.Provider)
		}
		seen[e.Provider] = true
	}
}

// TestAll_ReturnsDefensiveCopy verifies a caller mutating the returned slice
// cannot corrupt the package's shared entries — All() copies rather than
// returning entries directly.
func TestAll_ReturnsDefensiveCopy(t *testing.T) {
	first := All()
	first[0].Name = "mutated"

	second := All()
	if second[0].Name == "mutated" {
		t.Fatal("mutating a slice returned by All() affected a later call — entries is not being copied")
	}
}
