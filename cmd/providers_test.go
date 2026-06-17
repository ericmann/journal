package cmd

import (
	"strings"
	"testing"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/synth"
)

func TestSynthClientOpenAI(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.SynthProvider = config.SynthProviderOpenAI
	cfg.SynthOpenAIBaseURL = "https://openrouter.ai/api/v1"
	cfg.SynthOpenAIModel = "google/gemma-3-27b-it:free"

	t.Setenv(config.OpenAIKeyEnv, "")
	if _, err := synthClient(cfg); err == nil {
		t.Error("expected missing-key error for openai synth")
	}
	t.Setenv(config.OpenAIKeyEnv, "sk-or-test")
	c, err := synthClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.(*synth.OpenAI); !ok {
		t.Errorf("client = %T, want *synth.OpenAI", c)
	}
}

func TestAnswerClientOpenAI(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.SynthProvider = config.SynthProviderOpenAI
	cfg.SynthOpenAIBaseURL = "https://openrouter.ai/api/v1"
	cfg.SynthOpenAIModel = "google/gemma-3-27b-it:free"

	t.Setenv(config.OpenAIKeyEnv, "")
	if _, ok, reason := answerClient(cfg); ok || reason == nil || !strings.Contains(reason.Error(), config.OpenAIKeyEnv) {
		t.Errorf("keyless openai answers: ok=%v reason=%v", ok, reason)
	}
	t.Setenv(config.OpenAIKeyEnv, "sk-or-test")
	c, ok, _ := answerClient(cfg)
	if !ok {
		t.Fatal("openai answers should be available with key set")
	}
	if _, isOpenAI := c.(*synth.OpenAI); !isOpenAI {
		t.Errorf("client = %T, want *synth.OpenAI", c)
	}
}

func TestNewEmbedderPerProvider(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.EmbedProvider = config.EmbedProviderOpenAI
	cfg.EmbedOpenAIBaseURL = "https://api.openai.com/v1"
	cfg.EmbedOpenAIModel = "text-embedding-3-small"
	if _, ok := newEmbedder(cfg).(*embed.OpenAI); !ok {
		t.Errorf("openai embed: newEmbedder = %T, want *embed.OpenAI", newEmbedder(cfg))
	}
	cfg.EmbedProvider = config.EmbedProviderOllama
	if _, ok := newEmbedder(cfg).(*embed.Ollama); !ok {
		t.Errorf("ollama embed: newEmbedder = %T, want *embed.Ollama", newEmbedder(cfg))
	}
}

func TestDoctorOpenAIChecks(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.SynthProvider = config.SynthProviderOpenAI
	cfg.SynthOpenAIModel = "google/gemma-3-27b-it:free"
	t.Setenv(config.OpenAIKeyEnv, "sk-test")
	if c := synthCheck(cfg); !c.OK || !strings.Contains(c.Detail, "openai-compatible") || !strings.Contains(c.Detail, cfg.SynthOpenAIModel) {
		t.Errorf("synthCheck openai = %+v", c)
	}

	cfg.EmbedProvider = config.EmbedProviderOpenAI
	cfg.EmbedOpenAIModel = "text-embedding-3-small"
	if c := embedOpenAICheck(cfg); !c.OK || !strings.Contains(c.Detail, "text-embedding-3-small") {
		t.Errorf("embedOpenAICheck with key = %+v", c)
	}
	t.Setenv(config.OpenAIKeyEnv, "")
	if c := embedOpenAICheck(cfg); c.OK {
		t.Errorf("embedOpenAICheck without key should fail the verdict: %+v", c)
	}
}
