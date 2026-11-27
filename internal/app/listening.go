package app

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"netease-music-mygo/internal/listening"
	"netease-music-mygo/internal/player"
)

type listeningWriter struct {
	mu      sync.Mutex
	flushMu sync.Mutex
	pending map[string]listening.Event
	store   *listening.Store
}

func (w *listeningWriter) add(e listening.Event) {
	w.mu.Lock()
	if old, ok := w.pending[e.ID]; !ok || e.Revision > old.Revision {
		w.pending[e.ID] = e
	}
	w.mu.Unlock()
}

func (w *listeningWriter) flush() error {
	w.flushMu.Lock()
	defer w.flushMu.Unlock()
	w.mu.Lock()
	batch := make([]listening.Event, 0, len(w.pending))
	for _, e := range w.pending {
		batch = append(batch, e)
	}
	w.mu.Unlock()
	for _, e := range batch {
		if err := w.store.Save(e); err != nil {
			return err
		}
		w.mu.Lock()
		if w.pending[e.ID].Revision == e.Revision {
			delete(w.pending, e.ID)
		}
		w.mu.Unlock()
	}
	return nil
}

type listeningState struct {
	recorder   listening.Recorder
	writer     *listeningWriter
	due        bool
	checkpoint time.Time
}

var listeningNow = time.Now
var listeningPosition = (*player.Player).ListeningPosition

func (a *app) startListening() {
	path := os.Getenv("CLOUDY_LISTENING_FILE")
	if path == "" {
		dir := os.Getenv("CLOUDY_PORTABLE_DIR")
		if dir == "" {
			var err error
			dir, err = mygo.App.Path(mygo.PathUserData)
			if err != nil {
				log.Printf("listening directory: %v", err)
				return
			}
		}
		path = filepath.Join(dir, "listening.jsonl")
	}
	a.listening.writer = &listeningWriter{store: listening.NewStore(path), pending: map[string]listening.Event{}}
	a.pl.ListeningBoundary = func(action string) {
		a.sampleListening(false)
		if action == "finish" {
			a.saveListening(true)
		}
		if action == "seek" {
			a.listening.recorder.Discontinuity()
		}
	}
}

func (a *app) sampleListening(start bool) {
	s := &a.listening
	if s.writer == nil {
		return
	}
	p := a.pl
	now := listeningNow()
	position, playing := listeningPosition(p)
	t := p.Current()
	if start && playing && s.recorder.Current != nil && now.UnixMilli()-s.recorder.Current.LastHeardMS >= 30*time.Minute.Milliseconds() {
		a.saveListening(true)
	}
	if s.recorder.Current == nil && start && playing && t != nil && t.ProgramID == 0 {
		s.recorder.Start(listening.Event{SongID: max(t.ID, 0), AlbumID: max(t.Album.ID, 0), Title: t.Name, Artist: t.ArtistNames(), Album: t.Album.Name, LocalPath: t.LocalPath, Position: max(int(t.No), 0), Disc: max(int(t.Cd), 0), DurationMS: max(t.Duration, 0)}, now, position, playing)
		if s.recorder.Current.DurationMS <= 0 {
			s.recorder.Current.DurationMS = p.Duration().Milliseconds()
		}
	}
	s.recorder.Sample(now, position, playing)
	if s.recorder.Current != nil && (now.Sub(s.checkpoint) >= 10*time.Second || !playing) {
		a.saveListening(false)
	}
	if playing && !s.due && !a.quitting {
		s.due = true
		sessionAfter(time.Second, func() {
			s.due = false
			if !a.quitting {
				a.sampleListening(true)
			}
		})
	}
}

func (a *app) saveListening(final bool) {
	s := &a.listening
	if s.writer == nil {
		return
	}
	if e := s.recorder.Snapshot(final); e != nil {
		s.checkpoint = listeningNow()
		s.writer.add(*e)
		if !a.quitting {
			go func() {
				if err := s.writer.flush(); err != nil {
					log.Printf("save listening: %v", err)
					a.post(func() { a.toast = "聆听记录保存失败"; a.redraw() })
				}
			}()
		}
	}
}

func (a *app) flushListening() {
	if a.listening.writer == nil {
		return
	}
	a.sampleListening(false)
	a.saveListening(true)
	if err := a.listening.writer.flush(); err != nil {
		log.Printf("flush listening: %v", err)
	}
}
