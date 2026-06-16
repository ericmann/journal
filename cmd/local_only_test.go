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

func TestLocalOnlyBlocksMCPNotSync(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.LocalOnly = true

	// MCP stays blocked under local_only (a client may forward to a cloud model).
	if err := runMCP(context.Background(), cfg, embed.NewFake(cfg.EmbedDim)); err == nil ||
		!strings.Contains(err.Error(), "local_only_mcp: allow") {
		t.Errorf("mcp under local_only: err = %v, want refusal with opt-in hint", err)
	}

	// Sync is NOT gated by local_only — it backs up to the user's own remote and
	// is governed by sync_enabled. Disabled: the normal opt-in message, never a
	// local_only refusal.
	cfg.SyncEnabled = false
	var out bytes.Buffer
	if err := runSync(context.Background(), cfg, embed.NewFake(cfg.EmbedDim), false, &out); err != nil {
		t.Fatalf("disabled sync should no-op, got: %v", err)
	}
	if strings.Contains(out.String(), "local_only") {
		t.Errorf("sync output must not mention local_only: %s", out.String())
	}

	// Enabled under local_only: it proceeds past any local_only gate (here it
	// fails later because the test repo has no git remote — NOT a local_only error).
	cfg.SyncEnabled = true
	if err := runSync(context.Background(), cfg, embed.NewFake(cfg.EmbedDim), true, &out); err != nil &&
		strings.Contains(err.Error(), "local_only") {
		t.Errorf("sync must not be blocked by local_only, got: %v", err)
	}
}

// local_only_mcp: allow is an explicit attestation that the MCP client is
// local; with it set, runMCP must get past the egress guard. The fake embedder
// is enough — runMCP fails later on the closed stdio transport, but NOT with
// the local_only refusal.
func TestLocalOnlyMCPAllowOptIn(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.LocalOnly = true
	cfg.LocalOnlyMCP = config.LocalOnlyMCPAllow

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // don't actually serve stdio; we only care about the guard
	err := runMCP(ctx, cfg, embed.NewFake(cfg.EmbedDim))
	if err != nil && strings.Contains(err.Error(), "local_only") {
		t.Errorf("mcp with local_only_mcp: allow should pass the guard, got: %v", err)
	}
}

func TestEgressCheckReportsPosture(t *testing.T) {
	cfg := testRepo(t, nil)

	c := egressCheck(cfg)
	if !c.OK || !strings.Contains(c.Detail, "anthropic") {
		t.Errorf("default egress check = %+v", c)
	}

	// local_only, mcp blocked, sync off → fully sealed.
	cfg.LocalOnly = true
	cfg.SynthProvider = config.SynthProviderOllama
	c = egressCheck(cfg)
	if !c.OK || !strings.Contains(c.Detail, "no cloud-AI egress") || !strings.Contains(c.Detail, "nothing leaves this machine") {
		t.Errorf("sealed local_only egress check = %+v", c)
	}

	// local_only + sync enabled: still no cloud-AI egress, but git backup runs,
	// so the "nothing leaves" guarantee must drop.
	cfg.SyncEnabled = true
	c = egressCheck(cfg)
	if !c.OK || !strings.Contains(c.Detail, "no cloud-AI egress") || !strings.Contains(c.Detail, "your git remote") {
		t.Errorf("local_only+sync egress check = %+v", c)
	}
	if strings.Contains(c.Detail, "nothing leaves this machine") {
		t.Errorf("guarantee must drop when sync is on: %+v", c)
	}

	// mcp attestation surfaces the client dependency.
	cfg.SyncEnabled = false
	cfg.LocalOnlyMCP = config.LocalOnlyMCPAllow
	c = egressCheck(cfg)
	if !c.OK || !strings.Contains(c.Detail, "depends on your MCP client") {
		t.Errorf("local_only+mcp-allow egress check = %+v", c)
	}
}

// doctor must make both synthesis paths discoverable: each provider's check
// names the active model and points at the other provider.
func TestSynthCheckSurfacesBothPaths(t *testing.T) {
	cfg := testRepo(t, nil)

	// anthropic (default): names the cloud model, points at ollama.
	c := synthCheck(cfg)
	if !c.OK || !strings.Contains(c.Detail, cfg.SynthModel) || !strings.Contains(c.Detail, "synth_provider: ollama") {
		t.Errorf("anthropic synth check = %+v", c)
	}

	// ollama: names the local model, points at anthropic.
	cfg.SynthProvider = config.SynthProviderOllama
	c = synthCheck(cfg)
	if !c.OK || !strings.Contains(c.Detail, cfg.SynthOllamaModel) || !strings.Contains(c.Detail, "synth_provider: anthropic") {
		t.Errorf("ollama synth check = %+v", c)
	}
}
