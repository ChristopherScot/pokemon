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

	repoName = "pokemon"

	// A pokedex-cli binary is a few MB; anything near this is not our asset.
	maxBinarySize = 100 * 1024 * 1024
)

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
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

	current := ensureV(Version)
	if Version == "dev" {
		fmt.Println("running a dev build; not updating")
		return nil
	}

	rel, err := latestRelease()
	if err != nil {
		return fmt.Errorf("check for updates: %w", err)
	}
	latest := ensureV(rel.TagName)
	if !isNewer(latest, current) {
		fmt.Println("already up to date")
		return nil
	}
	fmt.Printf("new version available: %s\n", rel.TagName)
	if checkOnly {
		fmt.Println("run 'pokedex-cli update' to install it")
		return nil
	}

	want := fmt.Sprintf("pokedex-cli_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
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

func latestRelease() (*githubRelease, error) {
	resp, err := http.Get(fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repoOwner, repoName))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned %s", resp.Status)
	}
	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func ensureV(v string) string {
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

func isNewer(latest, current string) bool {
	if !semver.IsValid(latest) || !semver.IsValid(current) {
		return false
	}
	return semver.Compare(latest, current) > 0
}

func installFrom(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %s", resp.Status)
	}

	exec, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	// Resolve symlinks so we replace the real file, not a link to it.
	exec, err = filepath.EvalSymlinks(exec)
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
			return fmt.Errorf("archive contained no pokedex-cli binary")
		}
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}
		if h.Name != "pokedex-cli" && h.Name != "./pokedex-cli" {
			continue
		}
		if h.Size > maxBinarySize {
			return fmt.Errorf("binary is %d bytes, over the %d limit", h.Size, maxBinarySize)
		}

		tmp := exec + ".new"
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

		old := exec + ".old"
		if err := os.Rename(exec, old); err != nil {
			os.Remove(tmp)
			return fmt.Errorf("move current binary aside: %w", err)
		}
		if err := os.Rename(tmp, exec); err != nil {
			os.Rename(old, exec) // put it back
			os.Remove(tmp)
			return fmt.Errorf("install new binary: %w", err)
		}
		os.Remove(old)
		return nil
	}
}
