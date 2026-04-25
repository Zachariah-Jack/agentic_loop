package appserver

import (
	"strings"
	"testing"

	"orchestrator/internal/config"
)

func TestBuildRequiredCodexExecProbeArgs(t *testing.T) {
	t.Parallel()

	args := buildRequiredCodexExecProbeArgs(`D:\repo`)
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"exec",
		"--model " + config.RequiredCodexExecutorModel,
		"--sandbox " + config.RequiredCodexSandboxMode,
		`approval_policy="never"`,
		`model_reasoning_effort="xhigh"`,
		`--cd D:\repo`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("probe args %q missing %q", joined, want)
		}
	}
}

func TestCodexProbeOutputOK(t *testing.T) {
	t.Parallel()

	if !codexProbeOutputOK("warning\nOK\ntokens used\n") {
		t.Fatal("codexProbeOutputOK() = false, want true for OK line")
	}
	if codexProbeOutputOK("NOT OK\n") {
		t.Fatal("codexProbeOutputOK() = true, want false for non-isolated OK")
	}
}

func TestParseCodexVersion(t *testing.T) {
	t.Parallel()

	if got := parseCodexVersion("codex-cli 0.124.0\n"); got != "codex-cli 0.124.0" {
		t.Fatalf("parseCodexVersion() = %q", got)
	}
}
