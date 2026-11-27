// Package listening keeps an incremental, durable journal of individual plays.
package listening

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Event struct {
	ID          string `json:"id"`
	Revision    int64  `json:"revision"`
	Sequence    int64  `json:"sequence"`
	SongID      int64  `json:"song_id"`
	AlbumID     int64  `json:"remote_album_id"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	LocalPath   string `json:"local_path"`
	Position    int    `json:"position"`
	Disc        int    `json:"disc"`
	DurationMS  int64  `json:"duration_ms"`
	StartedMS   int64  `json:"started_ms"`
	LastHeardMS int64  `json:"last_heard_ms"`
	ListenedMS  int64  `json:"listened_ms"`
	Final       bool   `json:"final"`
}

type Page struct {
	SourceID string  `json:"source_id"`
	Cursor   int64   `json:"cursor"`
	Events   []Event `json:"events"`
	HasMore  bool    `json:"has_more"`
}

func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

// Store is shared by background writers and the authenticated local API.
// Cloudy's single-instance lock owns the journal for this profile.
type Store struct {
	mu      sync.Mutex
	path    string
	source  string
	updates []Event
	latest  map[string]int64
}

func NewStore(path string) *Store { return &Store{path: path} }

func (s *Store) load() error {
	if s.source != "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	raw, err := os.ReadFile(s.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		// An interrupted append can only damage the final line. Keep its bytes
		// before restoring the last committed newline.
		end := bytes.LastIndexByte(raw, '\n') + 1
		if err := os.WriteFile(s.path+".interrupted-"+ID(), raw[end:], 0600); err != nil {
			return err
		}
		if err := os.Truncate(s.path, int64(end)); err != nil {
			return err
		}
		raw = raw[:end]
	}
	latest := map[string]int64{}
	var updates []Event
	source := ""
	for i, line := range bytes.Split(bytes.TrimSpace(raw), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		if i == 0 {
			var header struct {
				Source  string `json:"source_id"`
				Version int    `json:"version"`
			}
			if json.Unmarshal(line, &header) != nil || header.Source == "" || header.Version != 1 {
				return errors.New("invalid listening journal header")
			}
			source = header.Source
			continue
		}
		var e Event
		if json.Unmarshal(line, &e) != nil || e.ID == "" || e.Sequence != int64(len(updates)+1) || e.Revision <= latest[e.ID] {
			return fmt.Errorf("invalid listening journal line %d", i+1)
		}
		updates = append(updates, e)
		latest[e.ID] = e.Revision
	}
	if source == "" {
		source = ID()
		header, _ := json.Marshal(map[string]any{"source_id": source, "version": 1})
		if err := s.appendLine(header); err != nil {
			return err
		}
	}
	s.source, s.updates, s.latest = source, updates, latest
	return nil
}

func (s *Store) appendLine(raw []byte) error {
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	start, err := f.Seek(0, 2)
	if err != nil {
		return err
	}
	_, err = f.Write(append(raw, '\n'))
	if err == nil {
		err = f.Sync()
	}
	if err != nil {
		_ = f.Truncate(start)
	}
	return err
}

func (s *Store) Save(e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return err
	}
	if e.Revision <= s.latest[e.ID] {
		return nil
	}
	e.Sequence = int64(len(s.updates) + 1)
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err = s.appendLine(raw); err != nil {
		return err
	}
	s.updates = append(s.updates, e)
	s.latest[e.ID] = e.Revision
	return nil
}

func (s *Store) Read(after int64, limit int) (Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return Page{}, err
	}
	if after < 0 || after > int64(len(s.updates)) || limit < 1 || limit > 500 {
		return Page{}, errors.New("invalid listening cursor or limit")
	}
	end := min(after+int64(limit), int64(len(s.updates)))
	events := append([]Event{}, s.updates[after:end]...)
	return Page{s.source, end, events, end < int64(len(s.updates))}, nil
}
