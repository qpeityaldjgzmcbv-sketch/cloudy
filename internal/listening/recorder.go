package listening

import "time"

// Recorder is main-thread state. Position samples count only elapsed, advancing
// audio. Explicit discontinuities prevent short forward seeks counting as audio.
type Recorder struct {
	Current  *Event
	at       time.Time
	position time.Duration
	playing  bool
}

func (r *Recorder) Start(e Event, now time.Time, position time.Duration, playing bool) {
	e.ID, e.Revision, e.StartedMS = ID(), 0, now.UnixMilli()
	e.LastHeardMS = e.StartedMS
	r.Current, r.at, r.position, r.playing = &e, now, position, playing
}

func (r *Recorder) Sample(now time.Time, position time.Duration, playing bool) {
	if r.Current == nil {
		return
	}
	elapsed, advance := now.Sub(r.at), position-r.position
	if r.playing && elapsed > 0 && advance > 0 && advance <= elapsed+250*time.Millisecond {
		r.Current.ListenedMS += min(elapsed, advance).Milliseconds()
		r.Current.LastHeardMS = now.UnixMilli()
	}
	r.at, r.position, r.playing = now, position, playing
}

func (r *Recorder) Discontinuity() { r.playing = false }

func (r *Recorder) Snapshot(final bool) *Event {
	if r.Current == nil {
		return nil
	}
	r.Current.Revision++
	r.Current.Final = final
	e := *r.Current
	if final {
		r.Current = nil
	}
	return &e
}
