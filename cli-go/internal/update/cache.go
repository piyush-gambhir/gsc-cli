package update

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	CacheFile     = "update-check.json"
	CheckInterval = 24 * time.Hour
	CheckTimeout  = 3 * time.Second
	NoticeWait    = time.Second
)

type Cache struct {
	CheckedAt       time.Time `json:"checked_at,omitzero"`
	AttemptedAt     time.Time `json:"attempted_at,omitzero"`
	LatestVersion   string    `json:"latest_version,omitempty"`
	Error           string    `json:"error,omitempty"`
	NotifiedVersion string    `json:"notified_version,omitempty"`
	NotifiedAt      time.Time `json:"notified_at,omitzero"`
}

func ReadCache(dir string) Cache {
	var c Cache
	b, err := os.ReadFile(filepath.Join(dir, CacheFile))
	if err == nil && json.Unmarshal(b, &c) == nil && (c.LatestVersion == "" || IsRelease(c.LatestVersion)) {
		return c
	}
	return Cache{}
}

func WriteCache(dir string, c Cache) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".update-check-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, CacheFile))
}

func Record(dir string, r *Release, checkErr error, now time.Time) Cache {
	c := ReadCache(dir)
	c.CheckedAt = now
	if checkErr != nil {
		c.Error = checkErr.Error()
	} else {
		c.LatestVersion, c.Error = r.Version(), ""
	}
	_ = WriteCache(dir, c)
	return c
}

func (c Cache) Due(now time.Time) bool {
	return expired(c.CheckedAt, now, CheckInterval) && expired(c.AttemptedAt, now, CheckInterval)
}

func (c Cache) ShouldNotify(current string, now time.Time) bool {
	if !Newer(c.LatestVersion, current) {
		return false
	}
	return c.NotifiedVersion != c.LatestVersion || expired(c.NotifiedAt, now, CheckInterval)
}

func expired(at, now time.Time, d time.Duration) bool {
	age := now.Sub(at)
	return age < 0 || age >= d
}

func Notice(current, latest, method string) string {
	return fmt.Sprintf("A new version of gsc is available: v%s -> v%s\nUpdate with: %s\nRelease notes: %s\n",
		strings.TrimPrefix(current, "v"), strings.TrimPrefix(latest, "v"), UpdateCommand(method), ReleaseURL(latest))
}
