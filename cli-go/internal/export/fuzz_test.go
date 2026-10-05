package export

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// FuzzLoadManifest checks the resume manifest decoder on arbitrary file
// contents: it never panics, always returns a usable day map, and a manifest it
// accepts survives a save and reload unchanged.
// `go test -run=^$ -fuzz=FuzzLoadManifest ./internal/export` explores further.
func FuzzLoadManifest(f *testing.F) {
	f.Add([]byte(`{"version":1,"request_hash":"abc","site":"sc-domain:example.com","request":{"startDate":"","endDate":"","dimensions":["date","query"]},"format":"csv","days":{"2026-10-01":{"file":"2026-10-01.csv","rows":2,"complete":true,"preliminary":false,"requests":1}}}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"days":null}`))
	f.Add([]byte(`{"days":{"2026-10-01":null}}`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`{"version":"1"}`))
	f.Add([]byte{0xff, 0xfe})
	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		path := filepath.Join(dir, manifestName)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		m, err := loadManifest(path)
		if err != nil {
			return
		}
		if m == nil || m.Days == nil {
			t.Fatalf("accepted manifest without a day map: %+v", m)
		}
		if err := saveManifest(path, m); err != nil {
			t.Fatal(err)
		}
		again, err := loadManifest(path)
		if err != nil || !reflect.DeepEqual(again, m) {
			t.Fatalf("manifest does not round-trip: %+v -> %+v (%v)", m, again, err)
		}
	})
}
