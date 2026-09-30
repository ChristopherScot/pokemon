package main

import "testing"

func TestVersionIsSet(t *testing.T) {
	if Version == "" {
		t.Error("Version is empty; the ldflags default was removed")
	}
}

func testRelease(tag string, draft, pre bool, assets ...string) githubRelease {
	r := githubRelease{TagName: tag, Draft: draft, Prerelease: pre}
	for _, n := range assets {
		r.Assets = append(r.Assets, struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		}{Name: n, BrowserDownloadURL: "https://example.invalid/" + n})
	}
	return r
}

func TestASiblingsReleaseIsNotAnUpdate(t *testing.T) {
	if tagPrefix == "" {
		t.Skip("single-service repo: every release here is ours")
	}
	got := pickRelease([]githubRelease{
		testRelease("sibling/v9.9.9", false, false, "sibling.apk"),
		testRelease(tagPrefix+"v1.2.3", false, false, assetName()),
	})
	if got == nil || got.TagName != tagPrefix+"v1.2.3" {
		t.Fatalf("picked %v, want %sv1.2.3 - a sibling's release is not an update for us",
			got, tagPrefix)
	}
}

func TestTheNewestOfOursWins(t *testing.T) {
	got := pickRelease([]githubRelease{
		testRelease(tagPrefix+"v2.0.0", false, false, assetName()),
		testRelease(tagPrefix+"v1.0.0", false, false, assetName()),
	})
	if got == nil || got.TagName != tagPrefix+"v2.0.0" {
		t.Fatalf("picked %v, want the newest", got)
	}
}

// Releases cut before tags were namespaced are still installable.
func TestLegacyUnprefixedReleasesStayReachable(t *testing.T) {
	if tagPrefix == "" {
		t.Skip("no prefix, so nothing is legacy")
	}
	got := pickRelease([]githubRelease{
		testRelease("v9.9.9", false, false, "somethingelse.apk"),
		testRelease("v0.1.2", false, false, assetName()),
	})
	if got == nil || got.TagName != "v0.1.2" {
		t.Fatalf("picked %v, want the untagged v0.1.2 that carries our asset", got)
	}
}

// /releases returns drafts and prereleases; /releases/latest did not.
func TestDraftsAndPrereleasesAreSkipped(t *testing.T) {
	got := pickRelease([]githubRelease{
		testRelease(tagPrefix+"v3.0.0", true, false, assetName()),
		testRelease(tagPrefix+"v2.9.0", false, true, assetName()),
		testRelease(tagPrefix+"v2.0.0", false, false, assetName()),
	})
	if got == nil || got.TagName != tagPrefix+"v2.0.0" {
		t.Fatalf("picked %v, want the newest fully-released version", got)
	}
}

// The prefix must not reach semver, or every release reads as a downgrade.
func TestTheVersionComparedHasNoPrefix(t *testing.T) {
	if got := releaseVersion(tagPrefix + "v1.2.3"); got != "v1.2.3" {
		t.Errorf("releaseVersion(%q) = %q, want v1.2.3", tagPrefix+"v1.2.3", got)
	}
	if got := releaseVersion("v1.2.3"); got != "v1.2.3" {
		t.Errorf("releaseVersion(bare) = %q, want v1.2.3", got)
	}
}
