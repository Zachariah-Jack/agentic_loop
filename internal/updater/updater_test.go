package updater

import "testing"

func TestCompareVersions(t *testing.T) {
	t.Parallel()

	if CompareVersions("v1.2.0", "v1.1.9") <= 0 {
		t.Fatal("v1.2.0 should be newer than v1.1.9")
	}
	if CompareVersions("v1.2.0", "1.2.0") != 0 {
		t.Fatal("v1.2.0 should equal 1.2.0")
	}
	if CompareVersions("v1.0.0", "v1.0.1") >= 0 {
		t.Fatal("v1.0.0 should be older than v1.0.1")
	}
}

func TestChooseReleaseSkipsPrereleasesUnlessEnabled(t *testing.T) {
	t.Parallel()

	releases := []githubRelease{
		{TagName: "v2.0.0-beta", Prerelease: true},
		{TagName: "v1.5.0"},
	}
	release, found := chooseRelease(releases, false)
	if !found || release.TagName != "v1.5.0" {
		t.Fatalf("chooseRelease(stable) = %#v, %t; want v1.5.0", release, found)
	}
	release, found = chooseRelease(releases, true)
	if !found || release.TagName != "v2.0.0-beta" {
		t.Fatalf("chooseRelease(prerelease) = %#v, %t; want v2.0.0-beta", release, found)
	}
}
