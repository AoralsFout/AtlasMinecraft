package server

import (
	"atlas/internal/player"
	"atlas/internal/proto"
	"atlas/internal/world"
	"math"
)

const (
	fallingBlockEntity = "minecraft:falling_block"
	fallingBlockTypeID = 40 // Java 1.21.1 entity_type registry
)

func fallingMotion(w *proto.Writer, entity world.WorldEntity) *proto.Writer {
	motion, _ := entity.Data["Motion"].Elements()
	for axis := 0; axis < 3; axis++ {
		var speed float64
		if len(motion) == 3 {
			speed, _ = motion[axis].Value.(float64)
		}
		if math.IsNaN(speed) {
			speed = 0
		}
		w.I16(int16(min(3.9, max(-3.9, speed)) * 8000))
	}
	return w
}

func fallingSpawnPayload(id int32, entity world.WorldEntity) []byte {
	block, _ := entity.Data.Object("BlockState")
	name, _ := block.Text("Name")
	properties, _ := block.Object("Properties")
	states := make(map[string]string, len(properties))
	for key := range properties {
		states[key], _ = properties.Text(key)
	}
	state, err := world.BlockStateID(name, states)
	if err != nil || state == world.StateAir {
		return nil
	}
	x, y, z := entity.Position[0], entity.Position[1], entity.Position[2]
	yaw, pitch := angleI8(entity.Rotation[0]), angleI8(entity.Rotation[1])
	w := proto.NewWriter().VarInt(id).UUID(entity.UUID).VarInt(fallingBlockTypeID).
		F64(x).F64(y).F64(z).I8(pitch).I8(yaw).I8(yaw).VarInt(int32(state))
	return fallingMotion(w, entity).Payload()
}

func tickingEntityType(name string) bool {
	return name == fallingBlockEntity || name == world.LightningEntity
}

func weatherEntityInRange(p *player.Player, entity world.WorldEntity) bool {
	var distance float64
	switch entity.Type {
	case world.LightningEntity:
		distance = 16 * 16
	case world.SkeletonHorseEntity:
		distance = 10 * 16
	default:
		return true
	}
	x, _, z, _, _, _ := p.Pos()
	return math.Abs(x-entity.Position[0]) <= distance && math.Abs(z-entity.Position[2]) <= distance
}

func tickingSpawnPayload(id int32, entity world.WorldEntity) []byte {
	if entity.Type != world.LightningEntity {
		return fallingSpawnPayload(id, entity)
	}
	xyz := entity.Position
	return proto.NewWriter().VarInt(id).UUID(entity.UUID).VarInt(64).
		F64(xyz[0]).F64(xyz[1]).F64(xyz[2]).I8(0).I8(0).I8(0).VarInt(0).I16(0).I16(0).I16(0).Payload()
}

func fallingChunk(entity world.WorldEntity) (int32, int32) {
	return int32(math.Floor(entity.Position[0] / 16)), int32(math.Floor(entity.Position[2] / 16))
}

// World snapshots come from the live index without asking any region actor.
// entityMu keeps view changes, packet publication and dimension transfers in order.
func (s *Server) publishTickingEntities(w *world.World, players []*player.Player) {
	snapshots := append(w.FallingEntities(), w.LightningEntities()...)
	s.entityMu.Lock()
	defer s.entityMu.Unlock()
	dimension := w.DimensionName()
	if len(snapshots) == 0 && s.trackedTicking == 0 {
		return
	}
	live := make(map[[16]byte]world.WorldEntity, len(snapshots))
	for _, entity := range snapshots {
		live[entity.UUID] = entity
	}
	for _, p := range players {
		sink := p.WorldPacketSinkInDimension(dimension)
		if sink == nil || !p.PlayReady() {
			continue
		}
		view := s.generatedViews[p]
		var removed []int32
		for key, id := range view {
			tracking := s.generatedEntities[key]
			if key.dimension != dimension || tracking == nil || !tickingEntityType(tracking.snapshot.Type) {
				continue
			}
			if entity, ok := live[key.uuid]; ok {
				if x, z := fallingChunk(entity); sink.IsChunkSent(x, z) && weatherEntityInRange(p, entity) {
					continue
				}
			}
			delete(view, key)
			s.releaseGeneratedEntity(key)
			p.MarkInvisible(id)
			removed = append(removed, id)
		}
		if len(removed) != 0 {
			sink.SendPacket(proto.SEntityDestroy, destroyPayload(removed))
		}
		for _, entity := range snapshots {
			cx, cz := fallingChunk(entity)
			if !sink.IsChunkSent(cx, cz) || !weatherEntityInRange(p, entity) {
				continue
			}
			key := generatedEntityKey{dimension, entity.UUID}
			tracking := s.generatedEntities[key]
			if id, visible := view[key]; visible {
				if tracking.snapshot.Revision != entity.Revision || tracking.snapshot.Position != entity.Position {
					position := proto.NewWriter().VarInt(id).F64(entity.Position[0]).F64(entity.Position[1]).F64(entity.Position[2]).
						I8(angleI8(entity.Rotation[0])).I8(angleI8(entity.Rotation[1])).Bool(false).Payload()
					velocity := fallingMotion(proto.NewWriter().VarInt(id), entity).Payload()
					sink.SendPacketsInChunk(cx, cz, player.OutPacket{ID: proto.SEntityTeleport, Payload: position}, player.OutPacket{ID: proto.SEntityVelocity, Payload: velocity})
				}
				continue
			}
			s.spawnTickingEntity(p, sink, dimension, entity)
			view = s.generatedViews[p]
		}
	}
	for _, entity := range snapshots {
		if tracking := s.generatedEntities[generatedEntityKey{dimension, entity.UUID}]; tracking != nil {
			tracking.snapshot = entity
		}
	}
}

func (s *Server) spawnTickingEntity(p *player.Player, sink *player.WorldSink, dimension string, entity world.WorldEntity) {
	key := generatedEntityKey{dimension, entity.UUID}
	tracking := s.generatedEntities[key]
	cx, cz := fallingChunk(entity)
	if tracking == nil {
		tracking = s.newEntityTracking(entity)
	}
	payload := tickingSpawnPayload(tracking.id, entity)
	if len(payload) == 0 || !sink.SendPacketsInChunk(cx, cz, player.OutPacket{ID: proto.SSpawnEntity, Payload: payload}) {
		return
	}
	s.recordTickingViewer(p, key, tracking)
}

// Requires entityMu. Every accepted delivery has one matching removal.
func (s *Server) recordTickingViewer(p *player.Player, key generatedEntityKey, tracking *generatedEntityTracking) {
	view := s.generatedEntityView(p)
	if s.generatedEntities[key] == nil {
		s.trackedTicking++
		s.generatedEntities[key] = tracking
	}
	tracking.viewers++
	view[key] = tracking.id
	p.MarkVisible(tracking.id)
}

// Natural lightning starts tracking in tickChunk, before its first entity
// tick changes the rod state and broadcasts the spark event.
func (s *Server) publishLightningSpawn(players []*player.Player, dimension string, entity world.WorldEntity) func() {
	tracking := s.newEntityTracking(entity)
	cx, cz := fallingChunk(entity)
	packet := player.OutPacket{ID: proto.SSpawnEntity, Payload: tickingSpawnPayload(tracking.id, entity)}
	delivered := make(map[*player.Player]*player.WorldSink)
	for _, p := range players {
		sink := p.WorldPacketSinkInDimension(dimension)
		if sink != nil && p.PlayReady() && weatherEntityInRange(p, entity) && sink.SendPacketsInChunk(cx, cz, packet) {
			delivered[p] = sink
		}
	}
	// Block placement takes entityMu before editing terrain. Record viewers
	// after the world tick releases its block lock to preserve that order.
	return func() {
		s.entityMu.Lock()
		defer s.entityMu.Unlock()
		key := generatedEntityKey{dimension, entity.UUID}
		for p, sink := range delivered {
			if sink.Current() {
				s.recordTickingViewer(p, key, tracking)
			}
		}
	}
}
