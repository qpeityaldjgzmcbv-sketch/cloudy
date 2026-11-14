package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/player"
)

// Where playback was left is kept in session.json in the user data
// directory: on quitting, and every little while as it plays, in case the
// app does not get to quit.

var sessionPath = func() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session.json"), nil
}

var sessionFallbackPath = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "cloudy-session.json"), nil
}

func sessionPaths() []string {
	var paths []string
	if p, e := sessionPath(); e == nil {
		paths = append(paths, p)
	}
	if p, e := sessionFallbackPath(); e == nil && (len(paths) == 0 || paths[0] != p) {
		paths = append(paths, p)
	}
	return paths
}

// saveSession keeps where playback is, if it moved on since last kept.
func (a *app) saveSession() {
	a.rememberCurrent()
	s, ok := a.pl.Session()
	if !ok {
		return
	}
	b, err := json.Marshal(s)
	if err != nil || string(b) == a.savedSession {
		return
	}
	for _, path := range sessionPaths() {
		if writeFileAtomic(path, b, 0o600) == nil {
			a.savedSession = string(b)
			return
		}
	}
	a.toast = "播放会话保存失败"

}

// sessionEvery is how long after playback changed, and how often as it
// plays, the session is kept.
const sessionEvery = 30 * time.Second

// keepSession saves the session a while after playback changed, and every
// while again as it plays. Paused, nothing is due, and the app sleeps.
func (a *app) keepSession() {
	if a.sessionDue {
		return
	}
	a.sessionDue = true
	sessionAfter(sessionEvery, func() {
		a.sessionDue = false
		a.saveSession()
		if a.pl.Playing() {
			a.keepSession()
		}
	})
}

// sessionAfter runs fn on the main thread after d, without redrawing a
// window as Update would.
var sessionAfter = func(d time.Duration, fn func()) {
	time.AfterFunc(d, func() { mygo.RunOnMain(fn) })
}

// restoreSession picks up playback where it was left, paused, once the
// listener's session is back: the songs they may play depend on it.
func (a *app) restoreSession() {
	if a.restored {
		return
	}
	a.restored = true
	var s player.Session
	var latest time.Time
	for _, path := range sessionPaths() {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var candidate player.Session
		if json.Unmarshal(raw, &candidate) == nil && len(candidate.Queue) > 0 && (latest.IsZero() || info.ModTime().After(latest)) {
			s, latest = candidate, info.ModTime()
			a.savedSession = string(raw)
		}
	}
	if latest.IsZero() {
		return
	}
	a.pl.Restore(s)
	if s.Radio != "" && a.pl.Current() != nil {
		a.pl.AttachRadio(s.Radio, a.radioFeed(s.Radio))
	}
}
