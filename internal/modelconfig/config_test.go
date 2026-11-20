package modelconfig

import (
	"context"
	"strings"
	"testing"
)

func TestNewDefaultsToOllamaWithoutAPIKey(t *testing.T) {
	t.Parallel()

	env := map[string]string{}
	llm, err := New(context.Background(), func(key string) string {
		return env[key]
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := llm.Name(), defaultOllamaModel; got != want {
		t.Fatalf("model name = %q, want %q", got, want)
	}
}

func TestNewAllowsGeminiOnlyWhenExplicitlySelected(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"MODEL_PROVIDER": ProviderGemini,
	}
	_, err := New(context.Background(), func(key string) string {
		return env[key]
	})
	if err == nil || !strings.Contains(err.Error(), "GOOGLE_API_KEY") {
		t.Fatalf("New() error = %v, want missing Gemini key error", err)
	}
}

func TestNewRejectsUnknownProvider(t *testing.T) {
	t.Parallel()

	_, err := New(context.Background(), func(key string) string {
		if key == "MODEL_PROVIDER" {
			return "unknown"
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported MODEL_PROVIDER") {
		t.Fatalf("New() error = %v, want unsupported provider error", err)
	}
}

func TestNewRejectsRemoteOllamaEndpoint(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"OLLAMA_BASE_URL": "http://example.com/v1",
	}
	_, err := New(context.Background(), func(key string) string {
		return env[key]
	})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("New() error = %v, want loopback restriction error", err)
	}
}

func TestNewRejectsModelNameWithWhitespace(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"OLLAMA_MODEL": "qwen3:4b ; malformed",
	}
	_, err := New(context.Background(), func(key string) string {
		return env[key]
	})
	if err == nil || !strings.Contains(err.Error(), "OLLAMA_MODEL") {
		t.Fatalf("New() error = %v, want invalid model name error", err)
	}
}
