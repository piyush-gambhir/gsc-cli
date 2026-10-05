// Package update finds, verifies, and installs gsc releases from GitHub, and
// keeps the once-a-day release check cache used by the update notice.
package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	Repo = "piyush-gambhir/gsc-cli"
	// SourceUpdate is how a build in a Go bin directory is updated. gsc has no
	// `go install` path (the main package is the cli-go module root, so go
	// install would name the binary cli-go); the documented source install is
	// `make install` into $GOBIN or $(go env GOPATH)/bin.
	SourceUpdate = "git pull && make install (in your gsc-cli checkout)"
	// MethodSelf and MethodGo are the install methods: gsc replaces itself, or
	// it lives in a Go bin directory and is rebuilt from source.
	MethodSelf = "self"
	MethodGo   = "go"

	// LatestTimeout bounds the latest-release lookup; the background check
	// cancels sooner (CheckTimeout).
	LatestTimeout = 15 * time.Second

	maxBinary = 64 << 20
)

var versionPattern = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)(?:-([0-9A-Za-z.-]+))?$`)

type Release struct {
	Tag string
	URL string
}

// Version is the tag without its leading v.
func (r *Release) Version() string { return strings.TrimPrefix(r.Tag, "v") }

// ReleaseURL is the release notes page for version (with or without a leading v).
func ReleaseURL(version string) string {
	return "https://github.com/" + Repo + "/releases/tag/v" + strings.TrimPrefix(version, "v")
}

// Source is where releases are looked up and downloaded. Tests point it at a
// local server.
type Source struct {
	Download  string // GitHub base URL for release pages and assets
	Transport http.RoundTripper
}

// GitHub is the real release source; a nil transport uses the default.
func GitHub(t http.RoundTripper) Source {
	return Source{Download: "https://github.com", Transport: t}
}

func (s Source) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "gsc-cli")
	h := &http.Client{Timeout: 60 * time.Second, Transport: s.Transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || req.URL.Scheme != "https" {
			return fmt.Errorf("unsafe or excessive release redirect")
		}
		return nil
	}}
	res, err := h.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		return nil, fmt.Errorf("%s not found (HTTP 404)", url)
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("%s returned HTTP %d", url, res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("release response exceeds size limit")
	}
	return b, nil
}

// Latest returns the latest published (non-prerelease) release. It reads the
// tag from the redirect of github.com/<repo>/releases/latest instead of the
// GitHub API, whose unauthenticated limit (60 requests an hour per IP) fails
// behind shared NAT, CI runners, and VPNs. The redirect is never followed.
func (s Source) Latest(ctx context.Context) (*Release, error) {
	r, err := s.latest(ctx)
	if err != nil {
		return nil, fmt.Errorf("checking the latest release: %w", err)
	}
	return r, nil
}

func (s Source) latest(ctx context.Context) (*Release, error) {
	page := s.Download + "/" + Repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, "GET", page, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "gsc-cli")
	h := &http.Client{Timeout: LatestTimeout, Transport: s.Transport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := h.Do(req)
	if err != nil {
		return nil, err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusFound {
		return nil, fmt.Errorf("%s returned HTTP %d, expected a redirect to the latest release", page, res.StatusCode)
	}
	location := res.Header.Get("Location")
	if location == "" {
		return nil, fmt.Errorf("%s redirected without a Location", page)
	}
	base, err := url.Parse(s.Download)
	if err != nil {
		return nil, err
	}
	loc, err := url.Parse(location)
	prefix := "/" + Repo + "/releases/tag/"
	if err != nil || loc.Scheme != base.Scheme || loc.Host != base.Host || !strings.HasPrefix(loc.Path, prefix) {
		return nil, fmt.Errorf("%s redirected to %q, not a %s release page", page, location, Repo)
	}
	tag := strings.TrimPrefix(loc.Path, prefix)
	if !strings.HasPrefix(tag, "v") || !IsRelease(tag) {
		return nil, fmt.Errorf("invalid release version %q", tag)
	}
	return &Release{Tag: tag, URL: ReleaseURL(tag)}, nil
}

// AssetName is the GoReleaser archive for a platform (cli-go/.goreleaser.yaml).
func AssetName(goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return "gsc-cli_" + goos + "_" + goarch + ext
}

// BinaryName is the executable inside the archive.
func BinaryName(goos string) string {
	if goos == "windows" {
		return "gsc.exe"
	}
	return "gsc"
}

// Fetch downloads the release archive for a platform, verifies it against
// checksums.txt, and returns the extracted gsc binary.
func (s Source) Fetch(ctx context.Context, r *Release, goos, goarch string) ([]byte, error) {
	if !strings.HasPrefix(r.Tag, "v") || !IsRelease(r.Tag) {
		return nil, fmt.Errorf("invalid release version %q", r.Tag)
	}
	asset := AssetName(goos, goarch)
	base := s.Download + "/" + Repo + "/releases/download/" + r.Tag + "/"
	checksums, err := s.get(ctx, base+"checksums.txt", 1<<20)
	if err != nil {
		return nil, err
	}
	archive, err := s.get(ctx, base+asset, maxBinary)
	if err != nil {
		return nil, err
	}
	if err := VerifyChecksum(archive, checksums, asset); err != nil {
		return nil, err
	}
	return ExtractBinary(archive, goos)
}

// VerifyChecksum refuses an archive whose SHA-256 does not match its single
// entry in checksums.txt.
func VerifyChecksum(archive []byte, checksums []byte, name string) error {
	wanted := ""
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			if wanted != "" {
				return fmt.Errorf("duplicate archive checksum")
			}
			wanted = fields[0]
		}
	}
	hash := sha256.Sum256(archive)
	if wanted == "" || !strings.EqualFold(wanted, hex.EncodeToString(hash[:])) {
		return fmt.Errorf("SHA-256 checksum verification failed for %s", name)
	}
	return nil
}

// ExtractBinary returns the gsc executable from a release archive (tar.gz, or
// zip on Windows). Only a top-level regular file with the exact binary name is
// accepted; nothing is written to disk, so archive paths are never used.
func ExtractBinary(archive []byte, goos string) ([]byte, error) {
	name := BinaryName(goos)
	if goos == "windows" {
		return extractZip(archive, name)
	}
	z, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer z.Close()
	r := tar.NewReader(z)
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Name != name && h.Name != "./"+name {
			continue
		}
		if h.Typeflag != tar.TypeReg || h.Size <= 0 || h.Size > maxBinary {
			return nil, fmt.Errorf("invalid %s binary in release", name)
		}
		return io.ReadAll(io.LimitReader(r, maxBinary))
	}
	return nil, fmt.Errorf("release does not contain the %s binary", name)
}

func extractZip(archive []byte, name string) ([]byte, error) {
	z, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	for _, f := range z.File {
		if f.Name != name && f.Name != "./"+name {
			continue
		}
		if !f.Mode().IsRegular() || f.UncompressedSize64 == 0 || f.UncompressedSize64 > maxBinary {
			return nil, fmt.Errorf("invalid %s binary in release", name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		b, err := io.ReadAll(io.LimitReader(rc, maxBinary+1))
		if err != nil {
			return nil, err
		}
		if len(b) > maxBinary {
			return nil, fmt.Errorf("invalid %s binary in release", name)
		}
		return b, nil
	}
	return nil, fmt.Errorf("release does not contain the %s binary", name)
}

// CheckWritable fails with advice when the executable's directory cannot hold
// the replacement, before anything is downloaded.
func CheckWritable(exe, goos string) error {
	dir := filepath.Dir(exe)
	f, err := os.CreateTemp(dir, ".gsc-update-*")
	if err != nil {
		return notWritable(dir, goos, err)
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

func notWritable(dir, goos string, err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	if goos == "windows" {
		return fmt.Errorf("cannot update gsc: %s is not writable (%v). Run gsc update from a terminal that can write there, or move gsc.exe to a folder you own; the installed gsc is unchanged", dir, err)
	}
	return fmt.Errorf("cannot update gsc: %s is not writable (%v). Re-run with sudo, or reinstall into a directory you own with the install script (installs to ~/.local/bin): curl -fsSL https://raw.githubusercontent.com/%s/main/install.sh | sh; the installed gsc is unchanged", dir, err, Repo)
}

// rename is os.Rename; tests make it fail to exercise the Windows rollback.
var rename = os.Rename

// Replace swaps the executable at exe for bin. The new file is written next to
// exe first, so a failure leaves the old binary in place. On Windows a running
// executable cannot be overwritten or deleted but can be renamed, so it moves
// aside to exe.old (removed on a later start by RemoveLeftover).
func Replace(exe string, bin []byte, goos string) error {
	dir := filepath.Dir(exe)
	f, err := os.CreateTemp(dir, ".gsc-update-*")
	if err != nil {
		return notWritable(dir, goos, err)
	}
	tmp := f.Name()
	defer os.Remove(tmp) // a no-op once the rename succeeds
	if _, err := f.Write(bin); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0755); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if goos != "windows" {
		return rename(tmp, exe)
	}
	old := exe + ".old"
	_ = os.Remove(old) // left by an earlier update
	if err := rename(exe, old); err != nil {
		return fmt.Errorf("moving the running gsc aside: %w", err)
	}
	if err := rename(tmp, exe); err != nil {
		if rerr := rename(old, exe); rerr != nil {
			return fmt.Errorf("installing the new gsc: %w; restoring the previous one also failed (%v): rename %s to %s", err, rerr, old, exe)
		}
		return fmt.Errorf("installing the new gsc: %w", err)
	}
	return nil
}

// RemoveLeftover deletes the exe.old a Windows update leaves behind,
// best-effort (it fails while that old process is still running).
func RemoveLeftover(exe string) {
	_ = os.Remove(exe + ".old")
}

// Method reports how gsc at exe is updated: MethodGo when it lives in a Go bin
// directory ($GOBIN, $GOPATH/bin, or ~/go/bin), otherwise MethodSelf.
func Method(exe string, getenv func(string) string, home string) string {
	dir := filepath.Clean(filepath.Dir(exe))
	var candidates []string
	if gobin := getenv("GOBIN"); gobin != "" {
		candidates = append(candidates, gobin)
	}
	for _, p := range filepath.SplitList(getenv("GOPATH")) {
		if p != "" {
			candidates = append(candidates, filepath.Join(p, "bin"))
		}
	}
	if home != "" {
		candidates = append(candidates, filepath.Join(home, "go", "bin"))
	}
	for _, c := range candidates {
		c = filepath.Clean(c)
		if resolved, err := filepath.EvalSymlinks(c); err == nil {
			if samePath(dir, resolved) {
				return MethodGo
			}
		}
		if samePath(dir, c) {
			return MethodGo
		}
	}
	return MethodSelf
}

func samePath(a, b string) bool {
	if filepath.Separator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// UpdateCommand is the command the notice and `gsc update` suggest.
func UpdateCommand(method string) string {
	if method == MethodGo {
		return SourceUpdate
	}
	return "gsc update"
}

// IsRelease reports whether v is a release version (semver, optional v
// prefix); "dev" and other local builds are not.
func IsRelease(v string) bool { return versionPattern.MatchString(v) }

// Newer reports whether latest is a higher version than current. It is false
// when either is not a release version.
func Newer(latest, current string) bool {
	l, lpre, ok1 := parse(latest)
	c, cpre, ok2 := parse(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	// A release outranks its pre-releases (1.0.0 > 1.0.0-rc.1).
	return lpre == "" && cpre != ""
}

func parse(v string) ([3]int, string, bool) {
	m := versionPattern.FindStringSubmatch(v)
	if m == nil {
		return [3]int{}, "", false
	}
	var n [3]int
	for i := range n {
		x, err := strconv.Atoi(m[i+1])
		if err != nil {
			return n, "", false
		}
		n[i] = x
	}
	return n, m[4], true
}
