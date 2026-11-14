package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"encoding/json"
	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// tempHistory keeps the songs played in a file of the test's own.
func tempHistory(t *testing.T) string {
	path := filepath.Join(t.TempDir(), "history.json")
	old := historyPath
	oldFallback := historyFallbackPath
	historyFallbackPath = func() (string, error) { return path + ".fallback", nil }
	t.Cleanup(func() { historyFallbackPath = oldFallback })
	historyPath = func() (string, error) { return path, nil }
	t.Cleanup(func() { historyPath = old })
	return path
}

func song(id int64, name string) netease.Track {
	return netease.Track{ID: id, Name: name, Duration: 200000}
}

// Advance both media position and an injected observation clock without audio or sleeps.
func historyHeardFor(t *testing.T, a *app, out *audio.Player, d time.Duration) {
	old := historyNow
	now := time.Unix(100, 0)
	historyNow = func() time.Time { return now }
	t.Cleanup(func() { historyNow = old })
	a.observeHistory()
	out.Seek(out.Position() + d)
	now = now.Add(d)
	a.observeHistory()
}

// A song played again moves to the head, once, and the list keeps to its
// limit; a file played tells apart from another by its path.
func TestAddPlayed(t *testing.T) {
	now := time.Now()
	var played []playedSong
	played = addPlayed(played, song(1, "a"), now)
	played = addPlayed(played, song(2, "b"), now)
	played = addPlayed(played, song(1, "a"), now)
	if len(played) != 2 || played[0].Track.ID != 1 || played[1].Track.ID != 2 {
		t.Fatalf("played: %+v", played)
	}
	played = addPlayed(played, netease.Track{Name: "x", LocalPath: `D:\x.flac`}, now)
	played = addPlayed(played, netease.Track{Name: "y", LocalPath: `D:\y.flac`}, now)
	if len(played) != 4 {
		t.Fatalf("two files played came in as %d songs", len(played))
	}
	for i := range historyLimit + 20 {
		played = addPlayed(played, song(int64(100+i), "n"), now)
	}
	if len(played) != historyLimit || played[0].Track.ID != int64(100+historyLimit+19) {
		t.Fatalf("kept %d, the latest %d", len(played), played[0].Track.ID)
	}
}

// A song counts once heard for a while, or to its end, and is kept in the
// file for the next time the app opens.
func TestRememberKeepsTheSongsPlayed(t *testing.T) {
	path := tempHistory(t)
	a := newTestApp()
	a.recent.loaded = false
	a.remember(song(1, "skipped"), 3*time.Second, false)
	a.remember(song(2, "heard"), historyAfter, false)
	a.remember(song(3, "short, to its end"), 5*time.Second, true)
	if len(a.recent.played) != 2 || a.recent.played[0].Track.ID != 3 || a.recent.played[1].Track.ID != 2 {
		t.Fatalf("played: %+v", a.recent.played)
	}
	// The file is written off the main thread.
	deadline := time.Now().Add(3 * time.Second)
	for {
		b := newTestApp()
		b.recent.loaded = false
		b.loadHistory()
		if len(b.recent.played) == 2 && b.recent.played[0].Track.Name == "short, to its end" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the file has %+v", b.recent.played)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(path + ".part"); err == nil {
		t.Error("a part file was left")
	}
}

// 最近播放 lists the song playing, then those played on this computer, and
// needs no account; 清空记录 asks again before it clears.
func TestRecentPageIsThisComputers(t *testing.T) {
	tempHistory(t)
	a := newTestApp()
	a.settings = defaultSettings()
	a.recent.played = []playedSong{{Track: song(2, "昨天听的")}, {Track: song(3, "前天听的")}}
	a.route = route{page: pageRecent}
	tt := ui.NewTester(a.view, 1280, 820)
	tt.Frame()
	if !tt.HasText("昨天听的") || !tt.HasText("在这台电脑上用 Cloudy 听过的 2 首") {
		t.Fatalf("the page shows %v", tt.Texts())
	}
	if err := tt.Click("清空记录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.recent.played) != 2 || !tt.HasText("再点一次清空") {
		t.Fatal("one click cleared the songs played")
	}
	if err := tt.Click("再点一次清空"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.recent.played) != 0 || !tt.HasText("还没有播放记录：在 Cloudy 里听过的歌会出现在这里") {
		t.Errorf("cleared, the page shows %v", tt.Texts())
	}
}

// Quitting on the current song used to show it in Recent but never persist it.
func TestQuitKeepsCurrentHistory(t *testing.T) {
	tempHistory(t)
	a := newTestApp()
	a.recent.loaded = false
	a.pl.Cue([]netease.Track{song(42, "current")})
	out := audio.NewPlayer(&silence{})
	defer out.Close()
	a.pl.Attach(out)
	historyHeardFor(t, a, out, 15*time.Second)
	a.quitting = true
	a.flushHistory()
	b := newTestApp()
	b.recent.loaded = false
	b.loadHistory()
	if len(b.recent.played) != 1 || b.recent.played[0].Track.ID != 42 {
		t.Fatalf("current song lost after reopening: %+v", b.recent.played)
	}
}

// Restored positions without loaded audio and quickly skipped songs stay uncounted.
func TestQuitDoesNotCountUnheardHistory(t *testing.T) {
	tempHistory(t)
	a := newTestApp()
	a.recent.loaded = false
	a.pl.Cue([]netease.Track{song(42, "unloaded")})
	a.pl.Hold(time.Minute)
	a.flushHistory()
	if len(a.recent.played) != 0 {
		t.Fatal("counted an unloaded session position")
	}
	out := audio.NewPlayer(&silence{})
	defer out.Close()
	a.pl.Attach(out)
	out.Seek(3 * time.Second)
	a.flushHistory()
	b := newTestApp()
	b.recent.loaded = false
	b.loadHistory()
	if len(b.recent.played) != 0 {
		t.Fatal("counted a quickly skipped song")
	}
}

// The final save must beat earlier background writes, including clearing history.
func TestHistoryFinalSaveWins(t *testing.T) {
	path := tempHistory(t)
	a := newTestApp()
	a.recent.loaded = true
	old, _ := json.Marshal([]playedSong{{Track: song(1, "old")}})
	oldGen := historyGen.Add(1)
	a.recent.played = []playedSong{{Track: song(2, "new")}}
	a.flushHistory()
	if err := writeHistory(path, old, oldGen); err != nil {
		t.Fatal(err)
	}
	b := newTestApp()
	b.recent.loaded = false
	b.loadHistory()
	if len(b.recent.played) != 1 || b.recent.played[0].Track.ID != 2 {
		t.Fatal("old save overwrote final save")
	}
	a.clearHistory()
	a.flushHistory()
	b.recent.loaded = false
	b.loadHistory()
	if len(b.recent.played) != 0 {
		t.Fatal("cleared history returned after reopening")
	}
}

// Saving a session periodically commits the current song without waiting for a switch.
func TestSessionKeepsCurrentHistory(t *testing.T) {
	tempHistory(t)
	oldSessionPath := sessionPath
	oldFallback := sessionFallbackPath
	sessionFallbackPath = func() (string, error) { return filepath.Join(t.TempDir(), "fallback-session.json"), nil }
	t.Cleanup(func() { sessionFallbackPath = oldFallback })
	sessionFile := filepath.Join(t.TempDir(), "session.json")
	sessionPath = func() (string, error) { return sessionFile, nil }
	t.Cleanup(func() { sessionPath = oldSessionPath })
	a := newTestApp()
	a.recent.loaded = false
	a.pl.Cue([]netease.Track{song(42, "current")})
	out := audio.NewPlayer(&silence{})
	defer out.Close()
	a.pl.Attach(out)
	historyHeardFor(t, a, out, 15*time.Second)
	a.saveSession()
	if len(a.recent.played) != 1 || a.recent.played[0].Track.ID != 42 {
		t.Fatal("current not recorded")
	}
	// Flush before test cleanup; production does this on quit too.
	a.flushHistory()
}

// A writable test path hid the real failure: user data can be read-only.
func TestHistoryReadOnlyPrimarySurvivesReopenAndClear(t *testing.T) {
	primary := tempHistory(t)
	// A file in place of the parent makes writes fail on every platform/account.
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	historyPath = func() (string, error) { return filepath.Join(blocked, "history.json"), nil }
	a := newTestApp()
	a.recent.loaded = false
	a.recent.played = []playedSong{{Track: song(9, "saved")}}
	a.recent.loaded = true
	a.flushHistory()
	b := newTestApp()
	b.recent.loaded = false
	b.loadHistory()
	if len(b.recent.played) != 1 || b.recent.played[0].Track.ID != 9 {
		t.Fatal("fallback did not survive reopen")
	}
	// Recovering write access must not cause an older primary to hide the fallback.
	historyPath = func() (string, error) { return primary, nil }
	b.quitting = true
	b.clearHistory()
	c := newTestApp()
	c.recent.loaded = false
	c.loadHistory()
	if len(c.recent.played) != 0 {
		t.Fatal("clear did not survive location recovery")
	}
}

// A loaded, paused restored track used to masquerade as minutes heard on every startup.
func TestRestoredPausedTrackIsNotNewHistory(t *testing.T) {
	tempHistory(t)
	a := newTestApp()
	a.recent.played = []playedSong{{Track: song(2, "actually heard")}}
	a.pl.Cue([]netease.Track{song(42, "restored")})
	out := audio.NewPlayer(&silence{})
	defer out.Close()
	a.pl.Attach(out)
	a.pl.TogglePlay() // Session restoration loads audio but leaves playback paused.
	out.Seek(time.Minute)
	a.observeHistory()
	a.rememberCurrent()
	a.rememberTracked(*a.pl.Current(), false)
	a.flushHistory()
	tracks := a.recentSongs()
	if len(tracks) != 1 || tracks[0].ID != 2 {
		t.Fatal("restored paused track was prepended")
	}
	b := newTestApp()
	b.recent.loaded = false
	b.loadHistory()
	if len(b.recent.played) != 1 || b.recent.played[0].Track.ID != 2 {
		t.Fatal("restored position was counted as listening")
	}
}

// Position jumps and pauses do not count as listening, but normal progress does.
func TestHistoryListeningIgnoresSeekAndPause(t *testing.T) {
	h := historyListening{}
	track := song(1, "song")
	now := time.Unix(100, 0)
	h.sample(&track, time.Minute, false, now)
	h.sample(&track, time.Minute, false, now.Add(time.Minute))
	if h.heard != 0 {
		t.Fatal("paused restoration counted")
	}
	now = now.Add(time.Minute)
	h.sample(&track, time.Minute, true, now)
	h.sample(&track, 3*time.Minute, true, now.Add(time.Second))
	if h.heard != 0 {
		t.Fatal("seek counted")
	}
	now = now.Add(time.Second)
	h.sample(&track, 3*time.Minute+10*time.Second, false, now.Add(10*time.Second))
	if h.heard != 10*time.Second {
		t.Fatalf("normal progress: %v", h.heard)
	}
	h.sample(&track, 3*time.Minute+10*time.Second, false, now.Add(time.Hour))
	if h.heard != 10*time.Second {
		t.Fatal("pause counted")
	}
	next := song(2, "next")
	h.sample(&next, 0, true, now.Add(time.Hour))
	if h.heard != 0 {
		t.Fatal("new track inherited previous listening")
	}
}
