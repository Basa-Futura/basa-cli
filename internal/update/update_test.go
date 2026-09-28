package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Version
		ok   bool
	}{
		{"0.1.7", Version{0, 1, 7}, true},  // what the release workflow stamps
		{"v0.1.7", Version{0, 1, 7}, true}, // the tag
		{"v1.20.300", Version{1, 20, 300}, true},
		{"dev", Version{}, false},                     // plain go build
		{"v0.1.6-3-gabc1234-dirty", Version{}, false}, // make build off a tag
		{"abc1234", Version{}, false},                 // make build with no tags
		{"v0.2.0-rc1", Version{}, false},              // a prerelease is not a release
		{"v0.1", Version{}, false},
		{"v0.1.+7", Version{}, false},
		{"v0..7", Version{}, false},
	} {
		got, ok := Parse(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Parse(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestLess(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"0.1.6", "0.1.7", true},
		{"0.1.7", "0.1.7", false},
		{"0.1.8", "0.1.7", false},
		{"0.1.9", "0.1.10", true}, // numeric, not lexical
		{"0.9.9", "1.0.0", true},
		{"1.0.0", "0.9.9", false},
	} {
		a, _ := Parse(tc.a)
		b, _ := Parse(tc.b)
		if got := a.Less(b); got != tc.want {
			t.Errorf("%s.Less(%s) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestAssetFor(t *testing.T) {
	for _, tc := range []struct {
		goos, goarch string
		want         string
		ok           bool
	}{
		{"darwin", "arm64", "basa-darwin-arm64", true},
		{"darwin", "amd64", "basa-darwin-amd64", true},
		{"linux", "arm64", "basa-linux-arm64", true},
		{"linux", "amd64", "basa-linux-amd64", true},
		{"windows", "amd64", "", false},
		{"linux", "386", "", false},
	} {
		got, ok := assetFor(tc.goos, tc.goarch)
		if got != tc.want || ok != tc.ok {
			t.Errorf("assetFor(%s, %s) = %q, %v; want %q, %v", tc.goos, tc.goarch, got, ok, tc.want, tc.ok)
		}
	}
}

// redirectTo answers /releases/latest the way github.com does.
func redirectTo(location string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/releases/latest" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, location, http.StatusFound)
	}
}

func TestLatestReadsTheTagFromTheRedirect(t *testing.T) {
	srv := httptest.NewServer(redirectTo("/releases/tag/v0.1.7"))
	defer srv.Close()

	got, err := (&Releases{BaseURL: srv.URL}).Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if want := (Version{0, 1, 7}); got != want {
		t.Errorf("Latest = %v, want %v", got, want)
	}
}

// A repository with no releases redirects to the releases index, whose last
// segment is "releases". install.sh once built a "vreleases" URL out of that.
func TestLatestWithNothingPublished(t *testing.T) {
	srv := httptest.NewServer(redirectTo("/releases"))
	defer srv.Close()

	_, err := (&Releases{BaseURL: srv.URL}).Latest(context.Background())
	if !errors.Is(err, ErrNoRelease) {
		t.Errorf("Latest error = %v, want ErrNoRelease", err)
	}
}

func TestLatestReportsAnHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	_, err := (&Releases{BaseURL: srv.URL}).Latest(context.Background())
	if err == nil || errors.Is(err, ErrNoRelease) {
		t.Fatalf("Latest error = %v, want an HTTP error distinct from ErrNoRelease", err)
	}
	if want := "GitHub returned HTTP 404 looking for the latest release"; err.Error() != want {
		t.Errorf("Latest error = %q, want %q", err, want)
	}
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// releaseServer publishes one asset and a checksums.txt claiming sumFor.
func releaseServer(t *testing.T, asset string, bin []byte, sumFor string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/download/v0.1.7/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sumFor + "  " + asset + "\n" + sum([]byte("other")) + "  basa-other\n"))
	})
	mux.HandleFunc("/releases/download/v0.1.7/"+asset, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bin)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadReturnsAVerifiedBinary(t *testing.T) {
	bin := []byte("new basa")
	srv := releaseServer(t, "basa-linux-amd64", bin, sum(bin))

	got, err := (&Releases{BaseURL: srv.URL}).Download(context.Background(), Version{0, 1, 7}, "basa-linux-amd64")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if string(got) != "new basa" {
		t.Errorf("Download = %q, want %q", got, "new basa")
	}
}

func TestDownloadRefusesAChecksumMismatch(t *testing.T) {
	srv := releaseServer(t, "basa-linux-amd64", []byte("tampered"), sum([]byte("new basa")))

	got, err := (&Releases{BaseURL: srv.URL}).Download(context.Background(), Version{0, 1, 7}, "basa-linux-amd64")
	if !errors.Is(err, ErrChecksum) {
		t.Errorf("Download error = %v, want ErrChecksum", err)
	}
	if got != nil {
		t.Errorf("Download returned %q alongside a mismatch; want nothing", got)
	}
}

func TestDownloadNeedsAChecksumEntryForTheAsset(t *testing.T) {
	bin := []byte("new basa")
	srv := releaseServer(t, "basa-linux-amd64", bin, sum(bin))

	_, err := (&Releases{BaseURL: srv.URL}).Download(context.Background(), Version{0, 1, 7}, "basa-darwin-arm64")
	if want := "checksums.txt for v0.1.7 has no entry for basa-darwin-arm64"; err == nil || err.Error() != want {
		t.Errorf("Download error = %v, want %q", err, want)
	}
}

func TestReplaceSwapsTheFileAndKeepsItsMode(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "basa")
	if err := os.WriteFile(exe, []byte("old basa"), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := Replace(exe, []byte("new basa")); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new basa" {
		t.Errorf("content = %q, want %q", got, "new basa")
	}
	info, err := os.Stat(exe)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Errorf("mode = %v, want %v", info.Mode().Perm(), os.FileMode(0o750))
	}

	// No temp file left beside it.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only basa", len(entries))
	}
}
