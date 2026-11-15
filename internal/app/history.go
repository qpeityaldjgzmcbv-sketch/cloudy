package app

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/netease"
)

// What the listener played in Cloudy on this computer is kept in
// history.json in the user data directory, the latest first, each song
// once, as the 最近播放 page lists it. A song counts once it is heard for
// a while, or to its end: one skipped past is not played.

// playedSong is a song played, and when it was last.
type playedSong struct {
	Track netease.Track `json:"track"`
	At    int64         `json:"at"` // Unix milliseconds
}

// historyLimit is how many songs are kept, and historyAfter how long one
// is heard before it counts.
const (
	historyLimit = 500
	historyAfter = 10 * time.Second
)

// historyPath is where the songs played are kept; tests replace it.
var historyPath = func() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "history.json"), nil
}

// Keep a stable portable fallback when the user-data directory is read-only.
var historyFallbackPath = func() (string, error) {
	if dir := os.Getenv("CLOUDY_PORTABLE_DIR"); dir != "" {
		return filepath.Join(dir, "cloudy-history.json"), nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "cloudy-history.json"), nil
}

func historyPaths() []string {
	var paths []string
	if p, err := historyPath(); err == nil {
		paths = append(paths, p)
	}
	if p, err := historyFallbackPath(); err == nil && (len(paths) == 0 || p != paths[0]) {
		paths = append(paths, p)
	}
	return paths
}

// historyKey tells songs apart: by NetEase's id, an episode by its
// program's, and a song only on this computer by its file.
func historyKey(t *netease.Track) string {
	if t.ID == 0 && t.ProgramID == 0 {
		return "file:" + t.LocalPath
	}
	return strconv.FormatInt(t.ID, 10) + "/" + strconv.FormatInt(t.ProgramID, 10)
}

// addPlayed puts t at the head of played, taking it out from further down,
// and keeps the list to historyLimit.
func addPlayed(played []playedSong, t netease.Track, at time.Time) []playedSong {
	key := historyKey(&t)
	out := make([]playedSong, 0, min(len(played)+1, historyLimit))
	out = append(out, playedSong{Track: t, At: at.UnixMilli()})
	for _, p := range played {
		if len(out) == historyLimit {
			break
		}
		if historyKey(&p.Track) != key {
			out = append(out, p)
		}
	}
	return out
}

// loadHistory reads the songs played, once.
func (a *app) loadHistory() {
	r := &a.recent
	if r.loaded {
		return
	}
	r.loaded = true
	var latest time.Time
	// Each file is a complete snapshot, including an empty/cleared history.
	// Choosing the latest valid snapshot avoids resurrecting cleared entries.
	for _, path := range historyPaths() {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var played []playedSong
		if json.Unmarshal(b, &played) == nil && (latest.IsZero() || info.ModTime().After(latest)) {
			r.played, latest = played, info.ModTime()
		}
	}
}

// remember adds a song the listener moved on from, or heard to its end, to
// the songs played, if they heard enough of it.
func (a *app) remember(t netease.Track, played time.Duration, ended bool) {
	if !ended && played < historyAfter {
		return
	}
	a.loadHistory()
	a.recent.played = addPlayed(a.recent.played, t, time.Now())
	a.saveHistory()
	a.redraw()
}

// clearHistory forgets the songs played.
func (a *app) clearHistory() {
	a.recent.played, a.recent.loaded = nil, true
	a.saveHistory()
}

// historyWrites keeps the file to the latest list written: writes run off
// the main thread, and one begun later never loses to one begun before.
var (
	historyWrites struct {
		sync.Mutex
		last int64
	}
	historyGen atomic.Int64
)

// historyListening measures advancing playback in this run, not a restored or
// sought-to position. All observations happen on the main thread.
type historyListening struct {
	track    *netease.Track
	at       time.Time
	position time.Duration
	playing  bool
	heard    time.Duration
}

var historyNow = time.Now

func (h *historyListening) sample(track *netease.Track, position time.Duration, playing bool, now time.Time) {
	if h.track != track {
		*h = historyListening{track: track, at: now, position: position, playing: playing}
		return
	}
	elapsed, advance := now.Sub(h.at), position-h.position
	if h.playing && elapsed > 0 && advance > 0 && advance <= elapsed+250*time.Millisecond {
		h.heard += min(advance, elapsed)
	}
	h.at, h.position, h.playing = now, position, playing
}
func (a *app) observeHistory() {
	p := a.pl
	a.recent.heard.sample(p.Current(), time.Duration(float64(p.Position())/p.Speed()), p.Ready() && !p.Loading() && p.Playing(), historyNow())
}
func (a *app) rememberTracked(track netease.Track, ended bool) {
	a.observeHistory()
	h := &a.recent.heard
	if h.track != nil && historyKey(h.track) == historyKey(&track) && h.heard > 0 {
		a.remember(track, h.heard, ended)
	}
}

// rememberCurrent keeps a sufficiently heard current song even if the app exits
// before switching tracks. Unloaded session positions do not count as listening.
func (a *app) rememberCurrent() {
	a.observeHistory()
	t := a.pl.Current()
	if t == nil || a.recent.heard.heard < historyAfter {
		return
	}
	a.loadHistory()
	if len(a.recent.played) > 0 && historyKey(&a.recent.played[0].Track) == historyKey(t) {
		return
	}
	a.remember(*t, a.recent.heard.heard, false)
}

// writeHistory serializes both background saves and the final synchronous save.
func writeHistory(path string, b []byte, gen int64) error {
	return writeHistoryAvailable([]string{path}, b, gen)
}

func writeHistoryAvailable(paths []string, b []byte, gen int64) error {
	w := &historyWrites
	w.Lock()
	defer w.Unlock()
	if gen <= w.last {
		return nil
	}
	var failures []error
	for _, path := range paths {
		if err := writeFileAtomic(path, b, 0o600); err != nil {
			failures = append(failures, err)
			continue
		}
		w.last = gen
		return nil
	}
	if len(failures) == 0 {
		return errors.New("no playback history location")
	}
	return errors.Join(failures...)
}

// saveHistory writes in the background during use, and waits when quitting.
func (a *app) saveHistory() { a.persistHistory(a.quitting) }

func (a *app) persistHistory(wait bool) {
	b, err := json.Marshal(a.recent.played)
	if err != nil {
		return
	}
	paths := historyPaths()
	gen := historyGen.Add(1)
	write := func() {
		if err := writeHistoryAvailable(paths, b, gen); err != nil {
			log.Printf("save playback history: %v", err)
			if !wait {
				a.post(func() { a.toast = "播放记录保存失败"; a.redraw() })
			}
		}
	}
	if wait {
		write()
	} else {
		go write()
	}
}

// flushHistory records the current song and commits the latest full list before
// the process ends; older queued background writes can no longer overwrite it.
func (a *app) flushHistory() {
	a.loadHistory()
	a.rememberCurrent()
	a.persistHistory(true)
}

// recentSongs are the songs the 最近播放 page lists: the one playing,
// though it has not counted yet, then those played.
func (a *app) recentSongs() []netease.Track {
	a.loadHistory()
	played := a.recent.played
	out := make([]netease.Track, 0, len(played)+1)
	a.observeHistory()
	cur := a.pl.Current()
	if cur != nil && a.recent.heard.heard == 0 && !(a.pl.Playing() && a.pl.Ready()) {
		cur = nil
	}
	if cur != nil {
		out = append(out, *cur)
	}
	for i := range played {
		if cur != nil && historyKey(&played[i].Track) == historyKey(cur) {
			continue
		}
		out = append(out, played[i].Track)
	}
	return out
}

// Authenticated diagnostics expose only storage metadata, never account credentials.
func historyFileDiagnostics() []map[string]any {
	files := make([]map[string]any, 0)
	for _, path := range historyPaths() {
		item := map[string]any{"path": path, "exists": false}
		if info, err := os.Stat(path); err == nil {
			item["exists"] = true
			item["modified"] = info.ModTime().UnixMilli()
			if raw, err := os.ReadFile(path); err == nil {
				var played []playedSong
				if json.Unmarshal(raw, &played) == nil {
					item["entries"] = len(played)
				} else {
					item["error"] = "记录格式无效"
				}
			} else {
				item["error"] = "无法读取记录"
			}
		}
		files = append(files, item)
	}
	return files
}
