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
	centers := make(map[string]map[world.ChunkPosition]struct{})
	for _, p := range s.Hub.Snapshot() {
		dimension, x, z, active := p.SimulationCenter()
		if active {
			if centers[dimension] == nil {
				centers[dimension] = make(map[world.ChunkPosition]struct{})
			}
			centers[dimension][world.ChunkPosition{X: x, Z: z}] = struct{}{}
		}
	}
	var errs []error
	for _, w := range s.Worlds() {
		name := w.DimensionName()
		if err := w.TickPlayers(centers[name], int32(s.Cfg.SimulationDistance)); err != nil {
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
