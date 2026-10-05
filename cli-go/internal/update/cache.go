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
	// CacheFile holds the last release check, in the config directory.
	CacheFile = "update-check.json"
	// CheckInterval is how long a check (successful or not) and a shown notice last.
	CheckInterval = 24 * time.Hour
	// CheckTimeout bounds the background release check.
	CheckTimeout = 3 * time.Second
	// NoticeWait is how long a command that started the day's check waits for
	// its answer after the output, so a fast command does not lose it.
	NoticeWait = time.Second
)

// Cache is update-check.json.
type Cache struct {
	CheckedAt       time.Time `json:"checked_at,omitzero"`
	AttemptedAt     time.Time `json:"attempted_at,omitzero"`
	LatestVersion   string    `json:"latest_version,omitempty"`
	Error           string    `json:"error,omitempty"`
	NotifiedVersion string    `json:"notified_version,omitempty"`
	NotifiedAt      time.Time `json:"notified_at,omitzero"`
}

// ReadCache returns the cache in dir; a missing or unreadable file is empty.
func ReadCache(dir string) Cache {
	var c Cache
	b, err := os.ReadFile(filepath.Join(dir, CacheFile))
	if err == nil && json.Unmarshal(b, &c) == nil && (c.LatestVersion == "" || IsRelease(c.LatestVersion)) {
		return c
	}
	return Cache{}
}

// WriteCache replaces the cache in dir atomically.
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

// Record stores the result of a release check, keeping the notice history and
// the last known version when the check failed.
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

// Due reports whether the background check should query GitHub. A check that
// never finished (the command exited first) also counts, so GitHub is asked at
// most once per CheckInterval.
func (c Cache) Due(now time.Time) bool {
	return expired(c.CheckedAt, now, CheckInterval) && expired(c.AttemptedAt, now, CheckInterval)
}

// ShouldNotify reports whether to print the notice: a newer release is known
// and its notice was not shown within the last CheckInterval.
func (c Cache) ShouldNotify(current string, now time.Time) bool {
	if !Newer(c.LatestVersion, current) {
		return false
	}
	return c.NotifiedVersion != c.LatestVersion || expired(c.NotifiedAt, now, CheckInterval)
}

// expired is true when at is older than d, or in the future (a clock change).
func expired(at, now time.Time, d time.Duration) bool {
	age := now.Sub(at)
	return age < 0 || age >= d
}

// Notice is the update notice printed on stderr after a command's output.
func Notice(current, latest, method string) string {
	return fmt.Sprintf("A new version of gsc is available: v%s -> v%s\nUpdate with: %s\nRelease notes: %s\n",
		strings.TrimPrefix(current, "v"), strings.TrimPrefix(latest, "v"), UpdateCommand(method), ReleaseURL(latest))
}
