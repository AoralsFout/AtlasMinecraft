package server

import (
	"atlas/internal/player"
	"atlas/internal/proto"
	"atlas/internal/world"
	"context"
	"log/slog"
	"math"
)

// TickPortals schedules bounded player work; destination generation and actor
// reads never run in the server's 20 Hz world-clock loop.
func (s *Server) TickPortals(ticks int64) {
	if ticks <= 0 {
		return
	}
	for _, p := range s.Hub.Snapshot() {
		if p.BeginPortalCheck() {
			go s.checkPlayerPortal(p, int32(min(ticks, 300)))
		}
	}
}

func (s *Server) playerPortalContact(p *player.Player, w *world.World) (world.BlockPosition, string, error) {
	x, y, z, _, _, _ := p.Pos()
	const epsilon = 1e-7
	var positions []world.BlockPosition
	for by := int32(math.Floor(y + epsilon)); by <= int32(math.Floor(y+playerHeight(p)-epsilon)); by++ {
		if by < w.MinBuildHeight() || by >= w.MaxBuildHeight() {
			continue
		}
		for bz := int32(math.Floor(z - .3 + epsilon)); bz <= int32(math.Floor(z+.3-epsilon)); bz++ {
			for bx := int32(math.Floor(x - .3 + epsilon)); bx <= int32(math.Floor(x+.3-epsilon)); bx++ {
				positions = append(positions, world.BlockPosition{X: bx, Y: by, Z: bz})
			}
		}
	}
	if len(positions) == 0 {
		return world.BlockPosition{}, "", nil
	}
	states, err := w.ReadLoadedBlocks(positions)
	if err != nil {
		return world.BlockPosition{}, "", err
	}
	for i, id := range states {
		state, err := world.BlockStateByID(id)
		if err != nil {
			return world.BlockPosition{}, "", err
		}
		switch state.Name {
		case "minecraft:nether_portal":
			if w.DimensionName() != "minecraft:the_end" {
				return positions[i], state.Name, nil
			}
		case "minecraft:end_portal":
			if y < float64(positions[i].Y)+.75 && y+playerHeight(p) > float64(positions[i].Y)+.375 {
				return positions[i], state.Name, nil
			}
		}
	}
	return world.BlockPosition{}, "", nil
}

func (s *Server) checkPlayerPortal(p *player.Player, ticks int32) {
	transferred := false
	defer func() { p.EndPortalCheck(transferred) }()
	p.MovementMu.Lock()
	w := s.PlayerWorld(p)
	source := p.WorldPacketSinkInDimension(w.DimensionName())
	if source == nil {
		p.MovementMu.Unlock()
		return
	}
	portal, kind, err := s.playerPortalContact(p, w)
	if err == nil && kind == "minecraft:end_portal" && w.DimensionName() == "minecraft:the_end" {
		defer p.MovementMu.Unlock()
		if !source.Current() || !p.PlayReady() {
			return
		}
		if p.BeginEndCredits() {
			s.updateVisibility(p)
			p.SendPacket(proto.SGameStateChange, proto.NewWriter().U8(proto.GameEventWinGame).F32(0).Payload())
			s.savePlayer(p)
		} else if p.AdvancePortalTick(true, ticks, 0) {
			if err := s.returnFromEndLocked(p, false); err != nil {
				p.SetPortalCooldown(20)
				slog.Warn("End return failed", "player", p.Name, "error", err)
			} else {
				transferred = true
			}
		}
		return
	}
	delay := int32(0)
	if kind == "minecraft:nether_portal" {
		delay = 1
	}
	ready := p.AdvancePortalTick(err == nil && kind != "", ticks, delay)
	dimension, x, y, z, yaw, pitch, flying := p.WorldPosition()
	height := float32(playerHeight(p))
	p.MovementMu.Unlock()
	if !ready {
		return
	}
	ctx, cancel := portalContext(p)
	defer cancel()
	position := world.PlayerPosition{Dimension: dimension, X: x, Y: y, Z: z, Yaw: yaw, Pitch: pitch, Flying: flying}
	var arrival world.PlayerPosition
	var destination *world.World
	var ticket world.BlockPosition
	switch kind {
	case "minecraft:nether_portal":
		name := "minecraft:the_nether"
		if dimension == name {
			name = "minecraft:overworld"
		}
		var ok bool
		destination, ok = s.WorldInDimension(name)
		if !ok {
			return
		}
		var transition world.NetherPortalTransition
		transition, err = destination.NetherPortalArrival(ctx, w, portal, position, [3]float64{}, .6, height)
		arrival = transition.Position
		ticket = transition.TicketPosition
	case "minecraft:end_portal":
		var ok bool
		destination, ok = s.WorldInDimension("minecraft:the_end")
		if !ok {
			return
		}
		arrival, err = destination.PrepareEndArrival()
		arrival.Pitch = pitch
		ticket = world.BlockPosition{X: int32(math.Floor(arrival.X)), Y: int32(math.Floor(arrival.Y)), Z: int32(math.Floor(arrival.Z))}
	default:
		return
	}
	if err == nil {
		p.MovementMu.Lock()
		defer p.MovementMu.Unlock()
		if !source.Current() || !p.PlayReady() {
			return
		}
		if _, current, e := s.playerPortalContact(p, w); e != nil || current != kind {
			return
		}
		rotation := [2]float32{arrival.Yaw, arrival.Pitch}
		err = s.teleportLocked(p, arrival.Dimension, [3]float64{arrival.X, arrival.Y, arrival.Z}, &rotation, false, nil)
	}
	if err != nil {
		p.SetPortalCooldown(20)
		slog.Warn("portal destination unavailable", "player", p.Name, "error", err)
		return
	}
	transferred = true
	destination.PlacePortalTicket(ticket)
	if kind == "minecraft:end_portal" {
		p.SendPacket(proto.SLevelEvent, proto.NewWriter().I32(1032).I64(0).I32(0).Bool(false).Payload())
	}
}

func portalContext(p *player.Player) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-p.Done:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func (s *Server) returnFromEndLocked(p *player.Player, afterCredits bool) error {
	point, personal, err := s.World.PlayerSpawn(p.UUID)
	if err != nil {
		return err
	}
	var arrival world.PlayerPosition
	var charge func() error
	found := false
	if w, ok := s.WorldInDimension(point.Dimension); personal && ok {
		arrival, charge, found, err = w.ResolvePersonalSpawn(point, afterCredits)
		if err != nil {
			return err
		}
	}
	if !found {
		ctx, cancel := portalContext(p)
		defer cancel()
		arrival, err = s.World.SharedRespawn(ctx)
		if err != nil {
			return err
		}
		if personal {
			p.SendPacket(proto.SGameStateChange, proto.NewWriter().U8(proto.GameEventNoRespawnBlock).F32(0).Payload())
		}
	}
	rotation := [2]float32{arrival.Yaw, arrival.Pitch}
	return s.teleportLocked(p, arrival.Dimension, [3]float64{arrival.X, arrival.Y, arrival.Z}, &rotation, afterCredits, charge)
}
