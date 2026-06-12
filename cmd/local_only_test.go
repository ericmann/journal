package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/synth"
)

// The local_only kill-switch must close every egress path: cloud synthesis,
// cloud search answers, git sync, and the MCP server (whose client may forward
// content to a cloud model).

func TestSynthClientProviders(t *testing.T) {
	cfg := testRepo(t, nil)

	// anthropic + local_only: refused with a pointer to the local provider.
	cfg.LocalOnly = true
	if _, err := synthClient(cfg); err == nil || !strings.Contains(err.Error(), "synth_provider: ollama") {
		t.Errorf("local_only anthropic synth err = %v, want provider hint", err)
	}

	// ollama provider works under local_only (and needs no key).
	cfg.SynthProvider = config.SynthProviderOllama
	c, err := synthClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.(*synth.Ollama); !ok {
		t.Errorf("client = %T, want *synth.Ollama", c)
	}

	// anthropic without local_only still requires the key.
	cfg.SynthProvider = config.SynthProviderAnthropic
	cfg.LocalOnly = false
	t.Setenv(config.AnthropicKeyEnv, "")
	if _, err := synthClient(cfg); err == nil {
		t.Error("expected missing-key error for anthropic provider")
	}
	t.Setenv(config.AnthropicKeyEnv, "sk-test")
	if c, err := synthClient(cfg); err != nil {
		t.Fatal(err)
	} else if _, ok := c.(*synth.Anthropic); !ok {
		t.Errorf("client = %T, want *synth.Anthropic", c)
	}
}

func TestAnswerClientRespectsProviderAndLocalOnly(t *testing.T) {
	cfg := testRepo(t, nil)

	// ollama provider: always available, no key involved.
	cfg.SynthProvider = config.SynthProviderOllama
	if _, ok, _ := answerClient(cfg); !ok {
		t.Error("ollama provider answers should be available")
	}

	// anthropic + local_only: unavailable with an explanatory reason.
	cfg.SynthProvider = config.SynthProviderAnthropic
	cfg.LocalOnly = true
	if _, ok, reason := answerClient(cfg); ok || reason == nil || !strings.Contains(reason.Error(), "local_only") {
		t.Errorf("local_only anthropic answers: ok=%v reason=%v", ok, reason)
	}

	// anthropic without a key: unavailable, key hint.
	cfg.LocalOnly = false
	t.Setenv(config.AnthropicKeyEnv, "")
	if _, ok, reason := answerClient(cfg); ok || reason == nil || !strings.Contains(reason.Error(), config.AnthropicKeyEnv) {
		t.Errorf("keyless anthropic answers: ok=%v reason=%v", ok, reason)
	}
}

func TestLocalOnlyDisablesSyncAndMCP(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.LocalOnly = true
	cfg.SyncEnabled = true

	var out bytes.Buffer
	err := runSync(context.Background(), cfg, embed.NewFake(cfg.EmbedDim), false, &out)
	if err == nil || !strings.Contains(err.Error(), "local_only") {
		t.Errorf("sync under local_only: err = %v, want local_only refusal", err)
	}

	err = runMCP(context.Background(), cfg, embed.NewFake(cfg.EmbedDim))
	if err == nil || !strings.Contains(err.Error(), "local_only") {
		t.Errorf("mcp under local_only: err = %v, want local_only refusal", err)
	}
}

func TestEgressCheckReportsPosture(t *testing.T) {
	cfg := testRepo(t, nil)

	c := egressCheck(cfg)
	if !c.OK || !strings.Contains(c.Detail, "anthropic") {
		t.Errorf("default egress check = %+v", c)
	}

	cfg.LocalOnly = true
	cfg.SynthProvider = config.SynthProviderOllama
	c = egressCheck(cfg)
	if !c.OK || !strings.Contains(c.Detail, "no note content leaves this machine") {
		t.Errorf("local_only egress check = %+v", c)
	}
}
