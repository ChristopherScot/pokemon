package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"
)

const (
	repoOwner = "ChristopherScot"

	// The repo that holds this service, which is where its releases
	// land. In a monorepo that is the PARENT repo, not the service - a
	// release is per repository, so naming the service here asks
	// api.github.com for a repository that does not exist.
	repoName = "pokemon"
	// Releases are per REPOSITORY, and a monorepo holds several
	// services that each cut their own. Without a prefix they share one
	// tag namespace: `latest` is then whichever service released most
	// recently, and two services reaching the same version number
	// attach their assets to ONE release, where the second
	// checksums.txt overwrites the first.
	//
	// So a tag here is "pokedex-tui/v1.2.3", and this binary considers
	// only the releases carrying that prefix.
	tagPrefix = "pokedex-tui/"
	// A pokedex-tui binary is a few MB; anything near this is not our asset.
	maxBinarySize = 100 * 1024 * 1024
)

type githubRelease struct {
	TagName string `json:"tag_name"`

	// /releases returns drafts and prereleases, unlike
	// /releases/latest which filters them out. Walking the list
	// ourselves means filtering them ourselves, or `update` offers to
	// install something not yet released.
	Draft      bool `json:"draft"`
	Prerelease bool `json:"prerelease"`

	Assets []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func updateCmd() *cobra.Command {
	var checkOnly bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "update this binary to the latest release",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runUpdate(checkOnly)
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "report whether an update exists, install nothing")
	return cmd
}

func runUpdate(checkOnly bool) error {
	fmt.Printf("current version: %s\n", Version)

	// A dev build has no meaningful version, and overwriting someone's
	// working-tree build with a release is never what they want. Checked
	// before the network call so the message is the real reason rather
	// than whatever the API happens to say.
	// semver wants the leading v, so normalise toward it rather than
	// stripping it off and putting it back.
	current := ensureV(Version)
	if Version == "dev" {
		fmt.Println("running a dev build; not updating")
		return nil
	}

	rel, err := latestRelease()
	if err != nil {
		return fmt.Errorf("check for updates: %w", err)
	}
	latest := releaseVersion(rel.TagName)
	if !isNewer(latest, current) {
		fmt.Println("already up to date")
		return nil
	}
	fmt.Printf("new version available: %s\n", latest)
	if checkOnly {
		fmt.Println("run 'pokedex-tui update' to install it")
		return nil
	}

	want := assetName()
	var url string
	for _, a := range rel.Assets {
		if a.Name == want {
			url = a.BrowserDownloadURL
			break
		}
	}
	if url == "" {
		return fmt.Errorf("release %s has no asset for %s/%s", rel.TagName, runtime.GOOS, runtime.GOARCH)
	}
	if err := installFrom(url); err != nil {
		return err
	}
	fmt.Printf("updated to %s\n", rel.TagName)
	return nil
}

// latestRelease is the newest release belonging to THIS tool.
//
// Not /releases/latest: that endpoint answers "the newest release in
// this repository", which in a monorepo is whichever sibling released
// most recently. Asking it here meant a tool offering to install an
// APK, and failing with "no asset for darwin/arm64" - a message that
// blames the release rather than the question.
//
// Releases come back newest-first, so the first match wins.
func latestRelease() (*githubRelease, error) {
	resp, err := http.Get(fmt.Sprintf("https://api.github.com/repos/%s/%s/releases?per_page=100", repoOwner, repoName))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned %s", resp.Status)
	}
	var rels []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rels); err != nil {
		return nil, err
	}
	rel := pickRelease(rels)
	if rel == nil {
		return nil, fmt.Errorf("no pokedex-tui release found in %s/%s", repoOwner, repoName)
	}
	return rel, nil
}

// pickRelease is the newest release this tool can actually install.
//
// A prefixed tag is the answer whenever there is one. Falling back to
// "carries an asset for this tool" covers releases cut before tags were
// namespaced, which are still perfectly installable - dropping them
// would strand anyone running one.
func pickRelease(rels []githubRelease) *githubRelease {
	want := assetName()
	var fallback *githubRelease
	for i := range rels {
		r := &rels[i]
		if r.Draft || r.Prerelease {
			continue
		}
		if tagPrefix != "" && strings.HasPrefix(r.TagName, tagPrefix) {
			return r
		}
		if tagPrefix == "" {
			return r
		}
		if fallback == nil && hasAsset(r, want) {
			fallback = r
		}
	}
	return fallback
}

func hasAsset(r *githubRelease, name string) bool {
	for _, a := range r.Assets {
		if a.Name == name {
			return true
		}
	}
	return false
}

func assetName() string {
	return fmt.Sprintf("pokedex-tui_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
}

// releaseVersion is the semver part of a tag, with any prefix removed,
// so "pokedex-tui/v1.2.3" and "v1.2.3" compare the same.
func releaseVersion(tag string) string {
	return ensureV(strings.TrimPrefix(tag, tagPrefix))
}

// ensureV normalises a version toward the leading "v" that
// golang.org/x/mod/semver requires. Release tags carry it and ldflags
// may not, so accept either spelling.
func ensureV(v string) string {
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

// isNewer reports whether latest supersedes current.
//
// x/mod/semver rather than a hand-rolled comparison: comparing dotted
// components pairwise reads "10" as older than "2" unless every
// component parses as an integer, so a hand-rolled version goes quiet
// the moment any component reaches double digits - it reports "already
// up to date" forever, which is the failure mode you never notice.
//
// semver.Compare is the same comparison the go command uses, including
// the rule that a prerelease sorts BEFORE its release.
//
// An unparseable version returns false: better to leave someone on a
// working binary than to talk them into replacing it based on a
// comparison that did not mean anything.
func isNewer(latest, current string) bool {
	if !semver.IsValid(latest) || !semver.IsValid(current) {
		return false
	}
	return semver.Compare(latest, current) > 0
}

// installFrom replaces the running binary. The rename dance matters: a
// running executable cannot be overwritten in place on every platform, but
// it can be renamed out of the way, so write beside it and swap. The old
// binary is kept until the swap succeeds so a failure is recoverable.
func installFrom(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %s", resp.Status)
	}

	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	// Resolve symlinks so we replace the real file, not a link to it.
	binPath, err = filepath.EvalSymlinks(binPath)
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("decompress: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("archive contained no pokedex-tui binary")
		}
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}
		if h.Name != "pokedex-tui" && h.Name != "./pokedex-tui" {
			continue
		}
		if h.Size > maxBinarySize {
			return fmt.Errorf("binary is %d bytes, over the %d limit", h.Size, maxBinarySize)
		}

		tmp := binPath + ".new"
		f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return fmt.Errorf("create %s: %w", tmp, err)
		}
		n, err := io.Copy(f, io.LimitReader(tr, maxBinarySize))
		f.Close()
		if err != nil {
			os.Remove(tmp)
			return fmt.Errorf("write %s: %w", tmp, err)
		}
		if n == maxBinarySize {
			os.Remove(tmp)
			return fmt.Errorf("binary exceeded the %d byte limit", maxBinarySize)
		}

		old := binPath + ".old"
		if err := os.Rename(binPath, old); err != nil {
			os.Remove(tmp)
			return fmt.Errorf("move current binary aside: %w", err)
		}
		if err := os.Rename(tmp, binPath); err != nil {
			os.Rename(old, binPath) // put it back
			os.Remove(tmp)
			return fmt.Errorf("install new binary: %w", err)
		}
		os.Remove(old)
		return nil
	}
}
