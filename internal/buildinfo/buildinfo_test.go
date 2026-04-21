package buildinfo

import "testing"

func TestCurrentFallsBackWhenMetadataIsUnset(t *testing.T) {
	originalVersion := Version
	originalRevision := Revision
	originalBuildTime := BuildTime
	t.Cleanup(func() {
		Version = originalVersion
		Revision = originalRevision
		BuildTime = originalBuildTime
	})

	Version = ""
	Revision = ""
	BuildTime = ""

	info := Current()
	if info.Version != "dev" {
		t.Fatalf("Version = %q, want dev", info.Version)
	}
	if info.Revision != "unknown" {
		t.Fatalf("Revision = %q, want unknown", info.Revision)
	}
	if info.BuildTime != "unknown" {
		t.Fatalf("BuildTime = %q, want unknown", info.BuildTime)
	}
}

func TestCurrentTrimsBuildMetadata(t *testing.T) {
	originalVersion := Version
	originalRevision := Revision
	originalBuildTime := BuildTime
	t.Cleanup(func() {
		Version = originalVersion
		Revision = originalRevision
		BuildTime = originalBuildTime
	})

	Version = " 1.2.3 "
	Revision = " abc123 "
	BuildTime = " 2026-04-21T16:00:00Z "

	info := Current()
	if info.Version != "1.2.3" {
		t.Fatalf("Version = %q, want 1.2.3", info.Version)
	}
	if info.Revision != "abc123" {
		t.Fatalf("Revision = %q, want abc123", info.Revision)
	}
	if info.BuildTime != "2026-04-21T16:00:00Z" {
		t.Fatalf("BuildTime = %q, want 2026-04-21T16:00:00Z", info.BuildTime)
	}
}
