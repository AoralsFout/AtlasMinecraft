package server

import (
	"atlas/internal/world"
	"errors"
	"fmt"
)

// TickWorlds advances all dimensions by one tick. Chunks are keyed by dimension;
// worlds without an entry advance only their clocks. A simulation error is
// reported with its dimension while the other worlds still get their tick.
func (s *Server) TickWorlds(chunks map[string]world.TickChunks) error {
	s.worldClockMu.Lock()
	defer s.worldClockMu.Unlock()
	var errs []error
	for _, w := range s.Worlds() {
		name := w.DimensionName()
		if err := w.Tick(chunks[name]); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Server) setDayTime(ticks int64, relative bool) {
	s.worldClockMu.Lock()
	defer s.worldClockMu.Unlock()
	if relative {
		_, current := s.World.Time()
		ticks += current
	}
	for _, w := range s.Worlds() {
		w.SetDayTime(ticks)
	}
}
