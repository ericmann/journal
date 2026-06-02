package cmd

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
)

func TestHintOllamaAddsSetupStepsWhenUnreachable(t *testing.T) {
	cfg := config.Default()
	wrapped := fmt.Errorf("ollama embed: %w at %s: dial tcp: connection refused", embed.ErrUnreachable, cfg.OllamaBaseURL)

	got := hintOllama(&cfg, wrapped)
	if got == nil {
		t.Fatal("expected an error")
	}
	msg := got.Error()
	for _, want := range []string{"https://ollama.com", "ollama pull " + cfg.EmbedModel, "journal doctor"} {
		if !strings.Contains(msg, want) {
			t.Errorf("hint missing %q in:\n%s", want, msg)
		}
	}
	// The original error must remain in the chain.
	if !errors.Is(got, embed.ErrUnreachable) {
		t.Error("hintOllama dropped the ErrUnreachable chain")
	}
}

func TestHintOllamaPassesThroughOtherErrors(t *testing.T) {
	cfg := config.Default()
	orig := errors.New("some other failure")
	if got := hintOllama(&cfg, orig); got != orig {
		t.Errorf("non-Ollama error should pass through unchanged, got %v", got)
	}
	if hintOllama(&cfg, nil) != nil {
		t.Error("nil error should stay nil")
	}
}
