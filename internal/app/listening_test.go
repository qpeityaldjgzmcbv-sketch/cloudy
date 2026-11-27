package app

import (
	"path/filepath"
	"testing"
	"time"

	"netease-music-mygo/internal/listening"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// Main-thread observations persist actual audio and the same-track restart as two plays.
func TestListeningPlaybackAndQuit(t *testing.T) {
	t.Setenv("CLOUDY_LISTENING_FILE", filepath.Join(t.TempDir(), "listening.jsonl"))
	oldNow, oldAfter, oldPosition := listeningNow, sessionAfter, listeningPosition
	now := time.Unix(1700000000, 0)
	position := time.Duration(0)
	listeningPosition = func(*player.Player) (time.Duration, bool) { return position, true }
	listeningNow = func() time.Time { return now }
	sessionAfter = func(time.Duration, func()) {}
	t.Cleanup(func() { listeningNow, sessionAfter, listeningPosition = oldNow, oldAfter, oldPosition })
	a := newTestApp()
	track := song(-42, "track")
	track.LocalPath = filepath.Join(t.TempDir(), "track.flac")
	a.pl.Cue([]netease.Track{track})
	a.startListening()
	a.sampleListening(true)
	if a.listening.recorder.Current == nil {
		t.Fatal("playing audio did not start a record")
	}
	now = now.Add(15 * time.Second)
	position = 15 * time.Second
	a.sampleListening(true)
	// A short seek must be excluded even though its delta resembles real audio.
	a.pl.ListeningBoundary("seek")
	now = now.Add(time.Second)
	position = 16 * time.Second
	a.sampleListening(true)
	now = now.Add(10 * time.Second)
	position = 26 * time.Second
	a.sampleListening(true)
	a.pl.ListeningBoundary("finish")
	position = 0
	a.sampleListening(true)
	now = now.Add(5 * time.Second)
	position = 5 * time.Second
	a.quitting = true
	a.flushListening()
	page, err := a.listening.writer.store.Read(0, 500)
	if err != nil {
		t.Fatal(err)
	}
	latest := map[string]listening.Event{}
	for _, e := range page.Events {
		latest[e.ID] = e
	}
	var total int64
	for _, e := range latest {
		if e.SongID != 0 || e.LocalPath != track.LocalPath {
			t.Fatal("local file did not retain a synchronizable identity")
		}
		if !e.Final {
			t.Fatal("quit did not finalize a play")
		}
		total += e.ListenedMS
	}
	if len(latest) != 2 || total != 30000 {
		t.Fatalf("plays %d, listened %dms", len(latest), total)
	}
}

// Older queued snapshots cannot replace a finalized play.
func TestListeningWriterRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "listening.jsonl")
	w := &listeningWriter{store: listening.NewStore(path), pending: map[string]listening.Event{}}
	w.add(listening.Event{ID: "play", Revision: 3, Final: true})
	w.add(listening.Event{ID: "play", Revision: 1})
	if err := w.flush(); err != nil {
		t.Fatal(err)
	}
	p, err := w.store.Read(0, 10)
	if err != nil || len(p.Events) != 1 || p.Events[0].Revision != 3 || len(w.pending) != 0 {
		t.Fatalf("%#v %v", p, err)
	}
}
