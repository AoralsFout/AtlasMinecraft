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

func fallingChunk(entity world.WorldEntity) (int32, int32) {
	return int32(math.Floor(entity.Position[0] / 16)), int32(math.Floor(entity.Position[2] / 16))
}

// World snapshots come from the live index without asking any region actor.
// entityMu keeps view changes, packet publication and dimension transfers in order.
func (s *Server) publishFallingEntities(w *world.World, players []*player.Player) {
	s.entityMu.Lock()
	defer s.entityMu.Unlock()
	dimension := w.DimensionName()
	snapshots := w.FallingEntities()
	if len(snapshots) == 0 && s.trackedFalling == 0 {
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
			if key.dimension != dimension || tracking == nil || tracking.snapshot.Type != fallingBlockEntity {
				continue
			}
			if entity, ok := live[key.uuid]; ok {
				if x, z := fallingChunk(entity); sink.IsChunkSent(x, z) {
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
			if !sink.IsChunkSent(cx, cz) {
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
			firstViewer := tracking == nil
			if firstViewer {
				tracking = &generatedEntityTracking{id: s.nextEntityID(), snapshot: entity}
			}
			payload := fallingSpawnPayload(tracking.id, entity)
			if len(payload) == 0 || !sink.SendPacketsInChunk(cx, cz, player.OutPacket{ID: proto.SSpawnEntity, Payload: payload}) {
				continue
			}
			view = s.generatedEntityView(p)
			s.generatedEntities[key] = tracking
			if firstViewer {
				s.trackedFalling++
			}
			tracking.viewers++
			view[key] = tracking.id
			p.MarkVisible(tracking.id)
		}
	}
	for _, entity := range snapshots {
		if tracking := s.generatedEntities[generatedEntityKey{dimension, entity.UUID}]; tracking != nil {
			tracking.snapshot = entity
		}
	}
}
