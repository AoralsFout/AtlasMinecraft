package server

import (
	"atlas/internal/player"
	"atlas/internal/proto"
	"atlas/internal/world"
	"cmp"
	"errors"
	"fmt"
	"slices"
)

// TickWorlds advances all dimensions using one snapshot of online players.
// An error in one dimension does not prevent the other worlds from ticking.
func (s *Server) TickWorlds() error {
	s.worldClockMu.Lock()
	defer s.worldClockMu.Unlock()
	byDimension := make(map[string][]world.SimulationPlayer)
	players := s.Hub.Snapshot()
	slices.SortFunc(players, func(a, b *player.Player) int { return cmp.Compare(a.EID, b.EID) })
	for _, p := range players {
		dimension, x, y, z, spectator, active := p.SimulationPosition3D()
		if active {
			byDimension[dimension] = append(byDimension[dimension], world.SimulationPlayer{X: x, Y: y, Z: z, Spectator: spectator})
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
		publish := func(previous, current world.WeatherLevels, changedRain bool) {
			publishWeather(players, name, previous, current, changedRain)
		}
		var registerViewers []func()
		s.trapPublishMu.Lock()
		added := func(bolt world.WorldEntity, trap *world.WorldEntity) {
			if trap != nil {
				s.trapRevision.Add(1)
				// Horse tracking shares entityMu with block placement. Publish
				// the pair in native order after releasing the world tick lock.
				registerViewers = append(registerViewers, func() { s.publishTrapStrike(players, name, *trap, bolt) })
			} else {
				registerViewers = append(registerViewers, s.publishLightningSpawn(players, name, bolt))
			}
		}
		if err := w.TickServerPlayers(byDimension[name], int32(s.Cfg.SimulationDistance), publish, added, func(change world.TrapChange) {
			registerViewers = append(registerViewers, func() { s.publishTrapChange(players, name, change) })
		}); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
		for _, register := range registerViewers {
			register()
		}
		s.trapPublishMu.Unlock()
		s.publishTickingEntities(w, players)
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

// Intensity changes are dimension-local. When isRaining changes, vanilla
// follows those packets with a start/stop and both raw levels for every player.
func publishWeather(players []*player.Player, dimension string, previous, current world.WeatherLevels, changedRain bool) {
	for _, p := range players {
		if !p.PlayReady() {
			continue
		}
		if sink := p.WorldPacketSinkInDimension(dimension); sink != nil {
			if previous.Rain != current.Rain {
				sink.SendPacket(proto.SGameStateChange, weatherPacket(7, current.Rain))
			}
			if previous.Thunder != current.Thunder {
				sink.SendPacket(proto.SGameStateChange, weatherPacket(8, current.Thunder))
			}
		}
		if changedRain {
			event := byte(2)
			if current.IsRaining() {
				event = 1
			}
			sink := p.WorldPacketSink()
			sink.SendPacket(proto.SGameStateChange, weatherPacket(event, 0))
			sink.SendPacket(proto.SGameStateChange, weatherPacket(7, current.Rain))
			sink.SendPacket(proto.SGameStateChange, weatherPacket(8, current.Thunder))
		}
	}
}
