package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/audio"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// The session is kept a while after playback changes and every while as
// it plays, and paused nothing more is due. It was kept by a ticker that
// woke the app, and redrew its window, every half minute even at rest.
func TestSessionRestsWhilePaused(t *testing.T) {
	tempSessionFiles(t)
	var due []func()
	defer func(after func(time.Duration, func())) { sessionAfter = after }(sessionAfter)
	sessionAfter = func(_ time.Duration, fn func()) { due = append(due, fn) }

	a := newTestApp()
	a.pl.Changed = a.keepSession
	a.pl.Cue([]netease.Track{{ID: 7, Name: "宝船", Duration: 200000}})
	out := audio.NewPlayer(&silence{})
	out.SetVolume(0)
	out.Play()
	a.pl.Attach(out)
	defer out.Close()

	a.pl.TogglePlay()
	a.pl.TogglePlay()
	if len(due) != 1 {
		t.Fatalf("%d saves due after pausing and playing", len(due))
	}
	due[0]()
	if len(due) != 2 {
		t.Fatalf("playing, %d saves due after one, want another", len(due)-1)
	}

	a.pl.TogglePlay()
	due[1]()
	if len(due) != 2 {
		t.Errorf("paused, %d more saves due", len(due)-2)
	}
}

func tempSessionFiles(t *testing.T) (string, string) {
	old, oldFallback := sessionPath, sessionFallbackPath
	primary, fallback := filepath.Join(t.TempDir(), "session.json"), filepath.Join(t.TempDir(), "fallback.json")
	sessionPath = func() (string, error) { return primary, nil }
	sessionFallbackPath = func() (string, error) { return fallback, nil }
	t.Cleanup(func() { sessionPath, sessionFallbackPath = old, oldFallback })
	return primary, fallback
}

// A read-only primary used to restore the same stale test song forever.
func TestSessionFallbackReplacesStaleRestore(t *testing.T) {
	_, fallback := tempSessionFiles(t)
	tempHistory(t)
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	sessionPath = func() (string, error) { return filepath.Join(blocker, "session.json"), nil }
	a := newTestApp()
	latest := song(9, "latest")
	latest.LocalPath = filepath.Join(t.TempDir(), "missing.flac")
	a.pl.Cue([]netease.Track{latest})
	a.saveSession()
	raw, err := os.ReadFile(fallback)
	if err != nil {
		t.Fatal(err)
	}
	var session player.Session
	if json.Unmarshal(raw, &session) != nil || len(session.Queue) != 1 || session.Queue[0].ID != 9 {
		t.Fatal("session was not saved to fallback")
	}
	b := newTestApp()
	b.restoreSession()
	if b.pl.Current() == nil || b.pl.Current().ID != 9 || b.pl.Playing() {
		t.Fatal("did not restore latest session paused")
	}
}
