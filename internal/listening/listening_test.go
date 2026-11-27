package listening

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Only advancing audio contributes, including short samples around seeks and pauses.
func TestRecorderActualTime(t *testing.T) {
	now := time.Unix(1700000000, 0)
	var r Recorder
	r.Start(Event{SongID: 42}, now, 0, true)
	r.Sample(now.Add(10*time.Second), 10*time.Second, true)
	r.Discontinuity()
	r.Sample(now.Add(11*time.Second), 11*time.Second, true)
	r.Sample(now.Add(21*time.Second), 21*time.Second, false)
	r.Sample(now.Add(31*time.Second), 21*time.Second, true)
	r.Sample(now.Add(41*time.Second), 31*time.Second, true)
	r.Sample(now.Add(42*time.Second), 90*time.Second, true)
	first := r.Snapshot(false)
	last := r.Snapshot(true)
	if first.ListenedMS != 30000 || last.ID != first.ID || last.Revision != 2 || !last.Final || r.Current != nil {
		t.Fatalf("snapshots %#v %#v", first, last)
	}
	r.Start(Event{SongID: 42}, now.Add(time.Minute), 0, true)
	if r.Current.ID == first.ID {
		t.Fatal("repeat reused its event id")
	}
}

// Reopening the journal preserves identity, pages and monotonic per-play revisions.
func TestJournalPagingAndRevisions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "listening.jsonl")
	s := NewStore(path)
	e := Event{ID: ID(), Revision: 1, StartedMS: 1}
	if err := s.Save(e); err != nil {
		t.Fatal(err)
	}
	p, err := s.Read(0, 1)
	if err != nil || p.Cursor != 1 || len(p.Events) != 1 {
		t.Fatalf("%#v %v", p, err)
	}
	e.Revision, e.ListenedMS = 3, 5000
	if err := s.Save(e); err != nil {
		t.Fatal(err)
	}
	e.Revision = 2
	if err := s.Save(e); err != nil {
		t.Fatal(err)
	}
	s = NewStore(path)
	q, err := s.Read(1, 1)
	if err != nil || q.SourceID != p.SourceID || q.Cursor != 2 || q.Events[0].ListenedMS != 5000 || q.HasMore {
		t.Fatalf("%#v %v", q, err)
	}
	if _, err := s.Read(9, 1); err == nil {
		t.Fatal("accepted cursor beyond journal")
	}
}

// A crash during the final append preserves committed lines and saves the torn bytes.
func TestJournalInterruptedTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "listening.jsonl")
	s := NewStore(path)
	if err := s.Save(Event{ID: ID(), Revision: 1}); err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	_, _ = f.WriteString(`{"id":"partial`)
	_ = f.Close()
	s = NewStore(path)
	p, err := s.Read(0, 10)
	if err != nil || len(p.Events) != 1 {
		t.Fatalf("%#v %v", p, err)
	}
	recovered, _ := filepath.Glob(path + ".interrupted-*")
	if len(recovered) != 1 {
		t.Fatal("interrupted bytes were not preserved")
	}
	raw, _ := os.ReadFile(recovered[0])
	if string(raw) != `{"id":"partial` {
		t.Fatalf("%q", raw)
	}
}

// Corruption in a committed line fails visibly without rewriting the file.
func TestJournalCommittedCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "listening.jsonl")
	raw := []byte("{\"source_id\":\"test\",\"version\":1}\ninvalid\n")
	_ = os.WriteFile(path, raw, 0600)
	if _, err := NewStore(path).Read(0, 10); err == nil {
		t.Fatal("accepted corrupt journal")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Fatal("rewrote corrupt journal")
	}
}
