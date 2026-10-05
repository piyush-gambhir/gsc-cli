package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type entry struct {
	name     string
	typeflag byte
	body     string
}

func tarGz(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	tw := tar.NewWriter(z)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0755, Typeflag: e.typeflag, Size: int64(len(e.body))}
		if e.typeflag == tar.TypeSymlink || e.typeflag == tar.TypeLink {
			h.Linkname, h.Size = "/etc/passwd", 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			tw.Write([]byte(e.body))
		}
	}
	tw.Close()
	z.Close()
	return b.Bytes()
}

func zipArchive(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		mode := os.FileMode(0755)
		if e.typeflag == tar.TypeSymlink {
			mode |= os.ModeSymlink
		}
		h.SetMode(mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(e.body))
	}
	zw.Close()
	return b.Bytes()
}

func checksumLine(archive []byte, name string) string {
	return fmt.Sprintf("%x  %s\n", sha256.Sum256(archive), name)
}

func TestChecksumAndExtraction(t *testing.T) {
	archive := tarGz(t, entry{"README.md", tar.TypeReg, "docs"}, entry{"gsc", tar.TypeReg, "binary"})
	name := "gsc-cli_darwin_arm64.tar.gz"
	if err := VerifyChecksum(archive, []byte(checksumLine(archive, name)), name); err != nil {
		t.Fatal(err)
	}
	if err := VerifyChecksum([]byte("tampered"), []byte(checksumLine(archive, name)), name); err == nil {
		t.Fatal("accepted a tampered archive")
	}
	if err := VerifyChecksum(archive, []byte(checksumLine(archive, "other.tar.gz")), name); err == nil {
		t.Fatal("accepted an archive with no checksum entry")
	}
	got, err := ExtractBinary(archive, "darwin")
	if err != nil || string(got) != "binary" {
		t.Fatalf("extract %q %v", got, err)
	}
	got, err = ExtractBinary(zipArchive(t, entry{"LICENSE", tar.TypeReg, "MIT"}, entry{"gsc.exe", tar.TypeReg, "exe"}), "windows")
	if err != nil || string(got) != "exe" {
		t.Fatalf("zip extract %q %v", got, err)
	}
}

func TestExtractRefusesTraversalAndNonRegularEntries(t *testing.T) {
	for name, archive := range map[string][]byte{
		"traversal":     tarGz(t, entry{"../gsc", tar.TypeReg, "evil"}),
		"absolute":      tarGz(t, entry{"/usr/local/bin/gsc", tar.TypeReg, "evil"}),
		"nested":        tarGz(t, entry{"sub/gsc", tar.TypeReg, "evil"}),
		"symlink":       tarGz(t, entry{"gsc", tar.TypeSymlink, ""}),
		"hardlink":      tarGz(t, entry{"gsc", tar.TypeLink, ""}),
		"directory":     tarGz(t, entry{"gsc", tar.TypeDir, ""}),
		"empty":         tarGz(t, entry{"gsc", tar.TypeReg, ""}),
		"wrong os name": tarGz(t, entry{"gsc.exe", tar.TypeReg, "exe"}),
	} {
		if b, err := ExtractBinary(archive, "linux"); err == nil {
			t.Errorf("%s: extracted %q", name, b)
		}
	}
	for name, archive := range map[string][]byte{
		"traversal": zipArchive(t, entry{"..\\gsc.exe", tar.TypeReg, "evil"}, entry{"../gsc.exe", tar.TypeReg, "evil"}),
		"symlink":   zipArchive(t, entry{"gsc.exe", tar.TypeSymlink, "C:\\Windows"}),
	} {
		if b, err := ExtractBinary(archive, "windows"); err == nil {
			t.Errorf("zip %s: extracted %q", name, b)
		}
	}
}

// releaseServer serves a release: latest tag, checksums.txt, and one archive.
func releaseServer(t *testing.T, tag, asset string, archive []byte, checksums string) Source {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/"+Repo+"/releases/tag/"+tag, http.StatusFound)
	})
	mux.HandleFunc("/"+Repo+"/releases/download/"+tag+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, checksums)
	})
	mux.HandleFunc("/"+Repo+"/releases/download/"+tag+"/"+asset, func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return Source{Download: srv.URL}
}

func TestFetchVerifiesTheReleaseBeforeExtracting(t *testing.T) {
	asset := AssetName("linux", "amd64")
	archive := tarGz(t, entry{"gsc", tar.TypeReg, "new binary"})
	src := releaseServer(t, "v0.2.0", asset, archive, checksumLine(archive, asset))
	r, err := src.Latest(context.Background())
	if err != nil || r.Tag != "v0.2.0" || r.Version() != "0.2.0" || r.URL != "https://github.com/"+Repo+"/releases/tag/v0.2.0" {
		t.Fatalf("latest %+v %v", r, err)
	}
	bin, err := src.Fetch(context.Background(), r, "linux", "amd64")
	if err != nil || string(bin) != "new binary" {
		t.Fatalf("fetch %q %v", bin, err)
	}

	tampered := releaseServer(t, "v0.2.0", asset, archive, checksumLine([]byte("something else"), asset))
	if _, err := tampered.Fetch(context.Background(), r, "linux", "amd64"); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("checksum mismatch accepted: %v", err)
	}
	missing := releaseServer(t, "v0.2.0", asset, archive, checksumLine(archive, "gsc-cli_linux_arm64.tar.gz"))
	if _, err := missing.Fetch(context.Background(), r, "linux", "amd64"); err == nil {
		t.Fatal("archive without a checksum entry accepted")
	}

	winAsset := AssetName("windows", "amd64")
	if winAsset != "gsc-cli_windows_amd64.zip" {
		t.Fatalf("windows asset %s", winAsset)
	}
	zipped := zipArchive(t, entry{"gsc.exe", tar.TypeReg, "new exe"})
	win := releaseServer(t, "v0.2.0", winAsset, zipped, checksumLine(zipped, winAsset))
	if bin, err := win.Fetch(context.Background(), r, "windows", "amd64"); err != nil || string(bin) != "new exe" {
		t.Fatalf("windows fetch %q %v", bin, err)
	}
}

// TestLatestReadsTheRedirect covers the github.com/<repo>/releases/latest
// lookup: only a 302 to this repo's semver release tag is accepted, and the
// redirect is never followed.
func TestLatestReadsTheRedirect(t *testing.T) {
	followed := 0
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { followed++ }))
	defer other.Close()
	var location string
	status := http.StatusFound
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+Repo+"/releases/latest" {
			followed++
			return
		}
		if location != "" {
			w.Header().Set("Location", strings.ReplaceAll(location, "SELF", srv.URL))
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	src := Source{Download: srv.URL}

	location = "SELF/" + Repo + "/releases/tag/v0.2.0"
	r, err := src.Latest(context.Background())
	if err != nil || r.Tag != "v0.2.0" || r.URL != "https://github.com/"+Repo+"/releases/tag/v0.2.0" {
		t.Fatalf("latest %+v %v", r, err)
	}

	for name, c := range map[string]struct {
		status   int
		location string
	}{
		"missing Location":   {http.StatusFound, ""},
		"foreign host":       {http.StatusFound, other.URL + "/" + Repo + "/releases/tag/v0.2.0"},
		"other scheme":       {http.StatusFound, "ftp" + strings.TrimPrefix(srv.URL, "http") + "/" + Repo + "/releases/tag/v0.2.0"},
		"relative":           {http.StatusFound, "/" + Repo + "/releases/tag/v0.2.0"},
		"other repo":         {http.StatusFound, "SELF/someone/else/releases/tag/v0.2.0"},
		"no releases":        {http.StatusFound, "SELF/" + Repo + "/releases"},
		"no v":               {http.StatusFound, "SELF/" + Repo + "/releases/tag/0.2.0"},
		"not semver":         {http.StatusFound, "SELF/" + Repo + "/releases/tag/v0.2"},
		"dev":                {http.StatusFound, "SELF/" + Repo + "/releases/tag/dev"},
		"path traversal":     {http.StatusFound, "SELF/" + Repo + "/releases/tag/v0.2.0/../../x"},
		"escaped slash":      {http.StatusFound, "SELF/" + Repo + "/releases/tag/v0.2.0%2Fx"},
		"rate limited":       {http.StatusForbidden, ""},
		"200 instead of 302": {http.StatusOK, "SELF/" + Repo + "/releases/tag/v0.2.0"},
		"permanent redirect": {http.StatusMovedPermanently, "SELF/" + Repo + "/releases/tag/v0.2.0"},
	} {
		status, location = c.status, c.location
		if r, err := src.Latest(context.Background()); err == nil || !strings.HasPrefix(err.Error(), "checking the latest release: ") {
			t.Errorf("%s: accepted %+v (%v)", name, r, err)
		}
	}
	if followed != 0 {
		t.Fatalf("followed the redirect %d times", followed)
	}
}

func writeExe(t *testing.T, dir, name, body string) string {
	t.Helper()
	exe := filepath.Join(dir, name)
	if err := os.WriteFile(exe, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestReplaceUnix(t *testing.T) {
	dir := t.TempDir()
	exe := writeExe(t, dir, "gsc", "old")
	if err := Replace(exe, []byte("new"), "linux"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, exe); got != "new" {
		t.Fatalf("exe holds %q", got)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(exe); info.Mode().Perm() != 0755 {
			t.Fatalf("mode %v", info.Mode())
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestReplaceWindowsRenamesTheRunningExeAside(t *testing.T) {
	dir := t.TempDir()
	exe := writeExe(t, dir, "gsc.exe", "old")
	writeExe(t, dir, "gsc.exe.old", "older") // left by an earlier update
	if err := Replace(exe, []byte("new"), "windows"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, exe); got != "new" {
		t.Fatalf("exe holds %q", got)
	}
	if got := read(t, exe+".old"); got != "old" {
		t.Fatalf("exe.old holds %q", got)
	}
	RemoveLeftover(exe)
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Fatalf("leftover not removed: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("unexpected files: %v", entries)
	}
}

func TestReplaceWindowsRestoresTheOldExeWhenTheMoveFails(t *testing.T) {
	dir := t.TempDir()
	exe := writeExe(t, dir, "gsc.exe", "old")
	rename = func(from, to string) error {
		if to == exe && from != exe+".old" {
			return fmt.Errorf("sharing violation")
		}
		return os.Rename(from, to)
	}
	t.Cleanup(func() { rename = os.Rename })
	if err := Replace(exe, []byte("new"), "windows"); err == nil || !strings.Contains(err.Error(), "sharing violation") {
		t.Fatalf("want the move error, got %v", err)
	}
	if got := read(t, exe); got != "old" {
		t.Fatalf("exe holds %q after rollback", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("leftover files after rollback: %v", entries)
	}
}

func TestUnwritableDirectoryKeepsTheOldBinary(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	dir := t.TempDir()
	exe := writeExe(t, dir, "gsc", "old")
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0755) })
	for _, goos := range []string{"linux", "windows"} {
		err := CheckWritable(exe, goos)
		if err == nil || !strings.Contains(err.Error(), "not writable") {
			t.Fatalf("%s check: %v", goos, err)
		}
		if goos == "linux" && (!strings.Contains(err.Error(), "sudo") || !strings.Contains(err.Error(), "install.sh")) {
			t.Fatalf("no advice: %v", err)
		}
		if err := Replace(exe, []byte("new"), goos); err == nil || !strings.Contains(err.Error(), "not writable") {
			t.Fatalf("%s replace: %v", goos, err)
		}
		if got := read(t, exe); got != "old" {
			t.Fatalf("old binary changed to %q", got)
		}
	}
}

func TestMethodDetectsGoBinDirectories(t *testing.T) {
	root := t.TempDir()
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	gopath := filepath.Join(root, "gopath")
	cases := []struct {
		exe  string
		env  map[string]string
		want string
	}{
		{filepath.Join(root, "gobin", "gsc"), map[string]string{"GOBIN": filepath.Join(root, "gobin")}, MethodGo},
		{filepath.Join(gopath, "bin", "gsc"), map[string]string{"GOPATH": filepath.Join(root, "first") + string(filepath.ListSeparator) + gopath}, MethodGo},
		{filepath.Join(root, "home", "go", "bin", "gsc"), nil, MethodGo},
		{filepath.Join(root, "home", ".local", "bin", "gsc"), map[string]string{"GOPATH": gopath}, MethodSelf},
		{filepath.Join(gopath, "gsc"), map[string]string{"GOPATH": gopath}, MethodSelf},
	}
	for _, c := range cases {
		if got := Method(c.exe, env(c.env), filepath.Join(root, "home")); got != c.want {
			t.Errorf("%s: got %s, want %s", c.exe, got, c.want)
		}
	}
	if UpdateCommand(MethodSelf) != "gsc update" || UpdateCommand(MethodGo) != SourceUpdate {
		t.Fatal("update commands")
	}
}

func TestVersions(t *testing.T) {
	for _, v := range []string{"0.1.3", "v0.1.3", "1.0.0-rc.1"} {
		if !IsRelease(v) {
			t.Errorf("%s is a release", v)
		}
	}
	for _, v := range []string{"", "dev", "0.1", "unknown", "0.1.3-dirty+x"} {
		if IsRelease(v) {
			t.Errorf("%q is not a release", v)
		}
	}
	for _, c := range []struct {
		latest, current string
		want            bool
	}{
		{"0.1.4", "0.1.3", true}, {"v0.2.0", "0.1.9", true}, {"0.1.10", "0.1.9", true}, {"1.0.0", "1.0.0-rc.1", true},
		{"0.1.3", "0.1.3", false}, {"0.1.3", "0.1.4", false}, {"0.1.4", "dev", false}, {"", "0.1.3", false},
	} {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.latest, c.current, got)
		}
	}
}

func TestCacheTimingAndNotice(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	if !(Cache{}).Due(now) {
		t.Fatal("empty cache must be due")
	}
	if (Cache{CheckedAt: now.Add(-23 * time.Hour)}).Due(now) {
		t.Fatal("a check within 24h is fresh")
	}
	if !(Cache{CheckedAt: now.Add(-25 * time.Hour)}).Due(now) {
		t.Fatal("a check older than 24h is due")
	}
	if (Cache{CheckedAt: now.Add(-25 * time.Hour), AttemptedAt: now.Add(-2 * time.Hour)}).Due(now) {
		t.Fatal("an unfinished attempt within 24h must not retry yet")
	}
	if !(Cache{AttemptedAt: now.Add(-25 * time.Hour)}).Due(now) {
		t.Fatal("an unfinished attempt older than 24h is due")
	}

	c := Cache{LatestVersion: "0.1.4"}
	if !c.ShouldNotify("0.1.3", now) || c.ShouldNotify("0.1.4", now) || c.ShouldNotify("dev", now) {
		t.Fatal("notify only when newer")
	}
	c.NotifiedVersion, c.NotifiedAt = "0.1.4", now.Add(-time.Hour)
	if c.ShouldNotify("0.1.3", now) {
		t.Fatal("shown within 24h")
	}
	if !c.ShouldNotify("0.1.3", now.Add(24*time.Hour)) {
		t.Fatal("show again after 24h")
	}
	c.LatestVersion = "0.1.5"
	if !c.ShouldNotify("0.1.3", now) {
		t.Fatal("a newer version is shown at once")
	}

	want := "A new version of gsc is available: v0.1.3 -> v0.1.4\nUpdate with: gsc update\nRelease notes: https://github.com/" + Repo + "/releases/tag/v0.1.4\n"
	if got := Notice("0.1.3", "0.1.4", MethodSelf); got != want {
		t.Fatalf("notice:\n%s", got)
	}
	if got := Notice("0.1.3", "0.1.4", MethodGo); !strings.Contains(got, "Update with: "+SourceUpdate+"\n") {
		t.Fatalf("go notice:\n%s", got)
	}
}

func TestRecordKeepsHistory(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	if err := WriteCache(dir, Cache{LatestVersion: "0.1.4", NotifiedVersion: "0.1.4", NotifiedAt: now}); err != nil {
		t.Fatal(err)
	}
	c := Record(dir, nil, fmt.Errorf("offline"), now)
	if c.LatestVersion != "0.1.4" || c.Error != "offline" || !c.CheckedAt.Equal(now) || c.NotifiedVersion != "0.1.4" {
		t.Fatalf("failed check %+v", c)
	}
	c = Record(dir, &Release{Tag: "v0.1.5"}, nil, now)
	if got := ReadCache(dir); got.LatestVersion != "0.1.5" || got.Error != "" || !got.NotifiedAt.Equal(now) || c.LatestVersion != "0.1.5" {
		t.Fatalf("cache %+v", got)
	}
	os.WriteFile(filepath.Join(dir, CacheFile), []byte(`{"latest_version":"../../x"}`), 0600)
	if ReadCache(dir) != (Cache{}) {
		t.Fatal("accepted an invalid cached version")
	}
}
