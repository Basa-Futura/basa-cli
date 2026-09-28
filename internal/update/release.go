package update

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"runtime"
	"strings"
)

// DefaultBaseURL is the upstream repository. Releases are published there and
// only there — a fork deliberately has none — so this is not configurable.
const DefaultBaseURL = "https://github.com/Basa-Futura/basa-cli"

// maxBinary bounds a download. The binary is about 7MB; this is headroom, not
// a target, and exists so a misbehaving server cannot fill the disk.
const maxBinary = 100 << 20

var (
	// ErrNoRelease means the repository is visible but has published nothing
	// that parses as a release.
	ErrNoRelease = errors.New("no release has been published yet")

	// ErrChecksum means the download does not match checksums.txt. Nothing is
	// installed when this is returned.
	ErrChecksum = errors.New("download does not match its published checksum")
)

// Releases reads published releases from a GitHub repository.
type Releases struct {
	BaseURL   string
	HTTP      *http.Client
	UserAgent string
}

// Latest resolves /releases/latest by its redirect, the way install.sh does.
//
// The redirect is read rather than the JSON API because the API allows 60
// unauthenticated requests an hour per address, shared by everyone behind the
// same office NAT. The web redirect has no such budget to exhaust.
func (r *Releases) Latest(ctx context.Context) (Version, error) {
	req, err := r.request(ctx, "/releases/latest")
	if err != nil {
		return Version{}, err
	}

	// A copy, so the caller's client keeps following redirects for downloads.
	client := *r.client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	resp, err := client.Do(req)
	if err != nil {
		return Version{}, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		// .../releases/tag/v0.1.7 for a release. A repository with none
		// redirects to .../releases instead, which fails to parse below.
		if v, ok := Parse(path.Base(resp.Header.Get("Location"))); ok {
			return v, nil
		}
		return Version{}, ErrNoRelease
	case resp.StatusCode == http.StatusOK:
		return Version{}, ErrNoRelease
	default:
		return Version{}, fmt.Errorf("GitHub returned HTTP %d looking for the latest release", resp.StatusCode)
	}
}

// Download fetches the binary for this platform at v and verifies it against
// the release's checksums.txt. The bytes are returned only if they match.
//
// As in install.sh, this proves the download arrived intact, not that the
// release is genuine: checksums.txt comes from the same release as the binary.
func (r *Releases) Download(ctx context.Context, v Version, asset string) ([]byte, error) {
	sums, err := r.fetch(ctx, v, "checksums.txt", 1<<20)
	if err != nil {
		return nil, err
	}
	want, ok := checksumFor(sums, asset)
	if !ok {
		return nil, fmt.Errorf("checksums.txt for %s has no entry for %s", v, asset)
	}

	bin, err := r.fetch(ctx, v, asset, maxBinary)
	if err != nil {
		return nil, err
	}

	got := sha256.Sum256(bin)
	if hex.EncodeToString(got[:]) != want {
		return nil, ErrChecksum
	}
	return bin, nil
}

func (r *Releases) fetch(ctx context.Context, v Version, name string, limit int64) ([]byte, error) {
	req, err := r.request(ctx, "/releases/download/"+v.String()+"/"+name)
	if err != nil {
		return nil, err
	}

	resp, err := r.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s for %s: HTTP %d", name, v, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("downloading %s for %s: %w", name, v, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("downloading %s for %s: larger than %d bytes", name, v, limit)
	}
	return body, nil
}

func (r *Releases) request(ctx context.Context, p string) (*http.Request, error) {
	base := r.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(base, "/")+p, nil)
	if err != nil {
		return nil, err
	}
	if r.UserAgent != "" {
		req.Header.Set("User-Agent", r.UserAgent)
	}
	return req, nil
}

func (r *Releases) client() *http.Client {
	if r.HTTP != nil {
		return r.HTTP
	}
	return http.DefaultClient
}

// checksumFor finds name in shasum output: "<hex>  <name>" per line.
func checksumFor(sums []byte, name string) (string, bool) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && fields[1] == name {
			return strings.ToLower(fields[0]), true
		}
	}
	return "", false
}

// Asset names the release binary for this platform, or reports that there is
// none. The list matches `make build-all`.
func Asset() (string, bool) {
	return assetFor(runtime.GOOS, runtime.GOARCH)
}

func assetFor(goos, goarch string) (string, bool) {
	switch goos + "/" + goarch {
	case "darwin/arm64", "darwin/amd64", "linux/arm64", "linux/amd64":
		return "basa-" + goos + "-" + goarch, true
	}
	return "", false
}
