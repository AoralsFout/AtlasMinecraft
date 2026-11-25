package server

import (
	"atlas/internal/world"
	"errors"
	"fmt"
)

// TickWorlds advances all dimensions using one snapshot of online players.
// An error in one dimension does not prevent the other worlds from ticking.
func (s *Server) TickWorlds() error {
	s.worldClockMu.Lock()
	defer s.worldClockMu.Unlock()
	byDimension := make(map[string][]world.SimulationPlayer)
	players := s.Hub.Snapshot()
	for _, p := range players {
		dimension, x, z, spectator, active := p.SimulationPosition()
		if active {
			byDimension[dimension] = append(byDimension[dimension], world.SimulationPlayer{X: x, Z: z, Spectator: spectator})
		}
	}
	var errs []error
	for _, w := range s.Worlds() {
		name := w.DimensionName()
		if err := w.TickPlayers(byDimension[name], int32(s.Cfg.SimulationDistance)); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
		s.publishFallingEntities(w, players)
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
