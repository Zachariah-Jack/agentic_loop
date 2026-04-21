package cli

import (
	"bytes"
	"context"
	"testing"

	"orchestrator/internal/buildinfo"
)

func TestRunVersionShowsBuildMetadata(t *testing.T) {
	originalVersion := buildinfo.Version
	originalRevision := buildinfo.Revision
	originalBuildTime := buildinfo.BuildTime
	t.Cleanup(func() {
		buildinfo.Version = originalVersion
		buildinfo.Revision = originalRevision
		buildinfo.BuildTime = originalBuildTime
	})

	buildinfo.Version = "1.4.0"
	buildinfo.Revision = "abc123def"
	buildinfo.BuildTime = "2026-04-21T17:00:00Z"

	var stdout bytes.Buffer
	err := runVersion(context.Background(), Invocation{
		Stdout:  &stdout,
		Stderr:  &bytes.Buffer{},
		Version: "1.4.0",
	})
	if err != nil {
		t.Fatalf("runVersion() error = %v", err)
	}

	for _, want := range []string{
		"version: 1.4.0",
		"revision: abc123def",
		"build_time: 2026-04-21T17:00:00Z",
	} {
		if !bytes.Contains(stdout.Bytes(), []byte(want)) {
			t.Fatalf("version output missing %q\n%s", want, stdout.String())
		}
	}
}
