package server

import (
	"atlas/internal/proto"
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
	s.serverTicks++
	syncTime := s.serverTicks%20 == 0
	// Vanilla synchronizes each dimension before ticking it. The Overworld
	// packet sees the previous age; derived levels see the newly advanced age.
	for _, w := range s.Worlds() {
		name := w.DimensionName()
		if syncTime {
			payload := timePayload(w)
			for _, p := range players {
				if sink := p.WorldPacketSinkInDimension(name); sink != nil && p.PlayReady() {
					sink.SendPacket(proto.SUpdateTime, payload)
				}
			}
		}
		if err := w.TickServerPlayers(byDimension[name], int32(s.Cfg.SimulationDistance)); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
		s.publishFallingEntities(w, players)
	}
	return errors.Join(errs...)
}

func (s *Server) setDayTime(ticks int64, relative bool) int64 {
	s.worldClockMu.Lock()
	defer s.worldClockMu.Unlock()
	if relative {
		_, current := s.World.Time()
		ticks += current
	}
	s.World.SetDayTime(ticks)
	return ticks
}
