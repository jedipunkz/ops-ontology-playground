package modelconfig

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/adk/v2/model/openaimodel"
	"google.golang.org/genai"
)

const (
	ProviderOllama = "ollama"
	ProviderGemini = "gemini"

	defaultOllamaBaseURL = "http://127.0.0.1:11434/v1"
	defaultOllamaModel   = "qwen3:4b"

	// Ollama requires a non-empty key for its OpenAI-compatible API and ignores
	// its value for local inference. This is a placeholder, not a credential.
	ollamaIgnoredAPIKey = "ollama"
)

var modelNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

func New(ctx context.Context, lookupEnv func(string) string) (model.LLM, error) {
	provider := strings.ToLower(strings.TrimSpace(lookupEnv("MODEL_PROVIDER")))
	if provider == "" {
		provider = ProviderOllama
	}

	switch provider {
	case ProviderOllama:
		baseURL := strings.TrimSpace(lookupEnv("OLLAMA_BASE_URL"))
		if baseURL == "" {
			baseURL = defaultOllamaBaseURL
		}
		if err := validateOllamaBaseURL(baseURL); err != nil {
			return nil, err
		}

		modelName := strings.TrimSpace(lookupEnv("OLLAMA_MODEL"))
		if modelName == "" {
			modelName = defaultOllamaModel
		}
		if !modelNamePattern.MatchString(modelName) {
			return nil, errors.New("OLLAMA_MODEL must be a valid local model name")
		}

		llm, err := openaimodel.NewModel(ctx, modelName, &openaimodel.ClientConfig{
			APIKey:  ollamaIgnoredAPIKey,
			BaseURL: baseURL,
			API:     openaimodel.APIChatCompletions,
			HTTPClient: &http.Client{
				Timeout: 5 * time.Minute,
				CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
					return http.ErrUseLastResponse
				},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("configure local Ollama model: %w", err)
		}
		return llm, nil

	case ProviderGemini:
		apiKey := strings.TrimSpace(lookupEnv("GOOGLE_API_KEY"))
		if apiKey == "" {
			return nil, errors.New("MODEL_PROVIDER=gemini requires GOOGLE_API_KEY")
		}
		llm, err := gemini.NewModel(ctx, "gemini-flash-latest", &genai.ClientConfig{APIKey: apiKey})
		if err != nil {
			return nil, fmt.Errorf("configure Gemini model: %w", err)
		}
		return llm, nil

	default:
		return nil, fmt.Errorf("unsupported MODEL_PROVIDER %q; choose %q or %q", provider, ProviderOllama, ProviderGemini)
	}
}

func validateOllamaBaseURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse OLLAMA_BASE_URL: %w", err)
	}
	if parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("OLLAMA_BASE_URL must be a plain local HTTP URL")
	}
	if !isLoopbackHost(parsed.Hostname()) {
		return errors.New("OLLAMA_BASE_URL must use localhost or a loopback IP")
	}
	if !strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), "/v1") {
		return errors.New("OLLAMA_BASE_URL must end with /v1")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
