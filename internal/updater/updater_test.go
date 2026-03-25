package updater

import (
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
)

func TestParseSemver_Valid(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []int
	}{
		{name: "simple", input: "1.2.3", want: []int{1, 2, 3}},
		{name: "with v prefix", input: "v1.2.3", want: []int{1, 2, 3}},
		{name: "zeros", input: "0.0.0", want: []int{0, 0, 0}},
		{name: "large numbers", input: "10.20.30", want: []int{10, 20, 30}},
		{name: "prerelease suffix", input: "v1.2.3-beta", want: []int{1, 2, 3}},
		{name: "build metadata", input: "v1.2.3+build.123", want: []int{1, 2, 3}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseSemver(tc.input)
			if got == nil {
				t.Fatalf("parseSemver(%q) = nil, want %v", tc.input, tc.want)
			}
			for i := 0; i < 3; i++ {
				if got[i] != tc.want[i] {
					t.Errorf("parseSemver(%q)[%d] = %d, want %d", tc.input, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestParseSemver_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty", input: ""},
		{name: "no dots", input: "123"},
		{name: "one dot", input: "1.2"},
		{name: "non-numeric", input: "a.b.c"},
		{name: "just v", input: "v"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseSemver(tc.input)
			if got != nil {
				t.Errorf("parseSemver(%q) = %v, want nil", tc.input, got)
			}
		})
	}
}

func TestIsNewerSemver(t *testing.T) {
	tests := []struct {
		name    string
		latest  string
		current string
		want    bool
		wantErr bool
	}{
		{name: "major bump", latest: "2.0.0", current: "1.0.0", want: true},
		{name: "minor bump", latest: "1.1.0", current: "1.0.0", want: true},
		{name: "patch bump", latest: "1.0.1", current: "1.0.0", want: true},
		{name: "equal versions", latest: "1.0.0", current: "1.0.0", want: false},
		{name: "older major", latest: "1.0.0", current: "2.0.0", want: false},
		{name: "older minor", latest: "1.0.0", current: "1.1.0", want: false},
		{name: "older patch", latest: "1.0.0", current: "1.0.1", want: false},
		{name: "with v prefix", latest: "v2.0.0", current: "v1.0.0", want: true},
		{name: "mixed v prefix", latest: "v1.1.0", current: "1.0.0", want: true},
		{name: "invalid latest", latest: "bad", current: "1.0.0", wantErr: true},
		{name: "invalid current", latest: "1.0.0", current: "bad", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := isNewerSemver(tc.latest, tc.current)
			if tc.wantErr {
				if err == nil {
					t.Error("isNewerSemver() should return error")
				}
				return
			}
			if err != nil {
				t.Fatalf("isNewerSemver() error: %v", err)
			}
			if got != tc.want {
				t.Errorf("isNewerSemver(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.want)
			}
		})
	}
}

func TestNew_CreatesUpdater(t *testing.T) {
	cfg := Config{
		CheckInterval: "12h",
		ReleaseURL:    "https://api.github.com/repos/owner/repo/releases/latest",
	}
	u := New(cfg, "1.0.0")

	if u == nil {
		t.Fatal("New() returned nil")
	}
	if u.currentVersion != "1.0.0" {
		t.Errorf("currentVersion = %q, want %q", u.currentVersion, "1.0.0")
	}
	if u.cfg.CheckInterval != "12h" {
		t.Errorf("cfg.CheckInterval = %q, want %q", u.cfg.CheckInterval, "12h")
	}
	if u.client == nil {
		t.Error("client should not be nil")
	}
}

func TestNew_EmptyConfig(t *testing.T) {
	u := New(Config{}, "0.1.0")
	if u == nil {
		t.Fatal("New() with empty config returned nil")
	}
	if u.cfg.ReleaseURL != "" {
		t.Errorf("ReleaseURL = %q, want empty", u.cfg.ReleaseURL)
	}
}

func TestSetCurrentVersion(t *testing.T) {
	u := New(Config{}, "1.0.0")
	u.SetCurrentVersion("2.0.0")

	u.mu.RLock()
	got := u.currentVersion
	u.mu.RUnlock()

	if got != "2.0.0" {
		t.Errorf("currentVersion after SetCurrentVersion = %q, want %q", got, "2.0.0")
	}
}

func TestLatestRelease_InitiallyNil(t *testing.T) {
	u := New(Config{}, "1.0.0")
	if rel := u.LatestRelease(); rel != nil {
		t.Errorf("LatestRelease() = %v, want nil initially", rel)
	}
}

func TestCheckNow_EmptyReleaseURL(t *testing.T) {
	u := New(Config{ReleaseURL: ""}, "1.0.0")
	_, err := u.CheckNow()
	if err == nil {
		t.Error("CheckNow() should return error when ReleaseURL is empty")
	}
}

func TestCheckNow_NonHTTPS(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "http URL", url: "http://example.com/releases"},
		{name: "ftp URL", url: "ftp://example.com/releases"},
		{name: "no scheme", url: "example.com/releases"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := New(Config{ReleaseURL: tc.url}, "1.0.0")
			_, err := u.CheckNow()
			if err == nil {
				t.Error("CheckNow() should reject non-HTTPS URLs")
			}
		})
	}
}

func TestFindChecksum(t *testing.T) {
	checksums := `abc123def456  soholink_linux_amd64.tar.gz
789abc012def  soholink_windows_amd64.exe
fedcba987654  soholink_darwin_amd64.tar.gz`

	tests := []struct {
		name     string
		assetURL string
		want     string
		wantErr  bool
	}{
		{
			name:     "linux binary",
			assetURL: "https://github.com/owner/repo/releases/download/v1.0.0/soholink_linux_amd64.tar.gz",
			want:     "abc123def456",
		},
		{
			name:     "windows binary",
			assetURL: "https://github.com/owner/repo/releases/download/v1.0.0/soholink_windows_amd64.exe",
			want:     "789abc012def",
		},
		{
			name:     "darwin binary",
			assetURL: "https://github.com/owner/repo/releases/download/v1.0.0/soholink_darwin_amd64.tar.gz",
			want:     "fedcba987654",
		},
		{
			name:     "missing asset",
			assetURL: "https://github.com/owner/repo/releases/download/v1.0.0/nonexistent.tar.gz",
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := findChecksum(checksums, tc.assetURL)
			if tc.wantErr {
				if err == nil {
					t.Error("findChecksum() should return error for missing asset")
				}
				return
			}
			if err != nil {
				t.Fatalf("findChecksum() error: %v", err)
			}
			if got != tc.want {
				t.Errorf("findChecksum() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSha256File(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/testfile"
	content := []byte("hello world")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	got, err := sha256File(path)
	if err != nil {
		t.Fatalf("sha256File() error: %v", err)
	}

	h := sha256.Sum256(content)
	want := fmt.Sprintf("%x", h[:])
	if got != want {
		t.Errorf("sha256File() = %q, want %q", got, want)
	}
}

func TestSha256File_NotFound(t *testing.T) {
	_, err := sha256File("/nonexistent/file")
	if err == nil {
		t.Error("sha256File() should return error for missing file")
	}
}

func TestReleaseStruct(t *testing.T) {
	r := Release{
		TagName:     "v1.2.3",
		AssetURL:    "https://example.com/asset.tar.gz",
		ChecksumURL: "https://example.com/checksums.txt",
	}
	if r.TagName != "v1.2.3" {
		t.Errorf("TagName = %q, want %q", r.TagName, "v1.2.3")
	}
	if r.AssetURL != "https://example.com/asset.tar.gz" {
		t.Errorf("AssetURL = %q", r.AssetURL)
	}
}

func TestConfigStruct(t *testing.T) {
	tests := []struct {
		name     string
		cfg      Config
		interval string
		url      string
	}{
		{name: "typical config", cfg: Config{CheckInterval: "24h", ReleaseURL: "https://api.github.com/repos/o/r/releases/latest"}, interval: "24h", url: "https://api.github.com/repos/o/r/releases/latest"},
		{name: "short interval", cfg: Config{CheckInterval: "1h", ReleaseURL: "https://example.com"}, interval: "1h", url: "https://example.com"},
		{name: "empty config", cfg: Config{}, interval: "", url: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cfg.CheckInterval != tc.interval {
				t.Errorf("CheckInterval = %q, want %q", tc.cfg.CheckInterval, tc.interval)
			}
			if tc.cfg.ReleaseURL != tc.url {
				t.Errorf("ReleaseURL = %q, want %q", tc.cfg.ReleaseURL, tc.url)
			}
		})
	}
}
