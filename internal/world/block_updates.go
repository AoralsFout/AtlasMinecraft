package world

import (
	"atlas/internal/blockrules"
	"atlas/internal/worldgen"
	"context"
	"errors"
	"strings"
)

var directionOffsets = [6]BlockPosition{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

func neighbor(p BlockPosition, face int) BlockPosition {
	o := directionOffsets[face]
	return BlockPosition{p.X + o.X, p.Y + o.Y, p.Z + o.Z}
}
func oppositeFace(face int) int { return face ^ 1 }
func FacingFace(facing string) int {
	switch facing {
	case "down":
		return 0
	case "up":
		return 1
	case "north":
		return 2
	case "south":
		return 3
	case "west":
		return 4
	case "east":
		return 5
	}
	return -1
}
func IsDoor(name string) bool {
	return strings.HasSuffix(name, "_door") && !strings.HasSuffix(name, "_trapdoor")
}

// PressStoneButton checks the interaction snapshot and persists the powered
// state together with its release. Repeated clicks do not renew the pulse.
func (w *World) PressStoneButton(p BlockPosition, expected uint32) error {
	state, err := BlockStateByID(expected)
	if err != nil || !worldgen.IsStoneButton(state.Name) {
		return errors.New("expected a stone button")
	}
	if state.Properties["powered"] == "true" {
		return nil
	}
	state.Properties["powered"] = "true"
	pressed, err := BlockStateID(state.Name, state.Properties)
	if err != nil {
		return err
	}
	w.blockUpdateMu.Lock()
	defer w.blockUpdateMu.Unlock()
	changed, err := w.editBlock(setBlockMsg{x: p.X, y: p.Y, z: p.Z, expected: &expected, state: pressed, loadedOnly: true, buttonRelease: state.Name})
	if err != nil {
		return err
	}
	if err := w.updateBlockNeighbors(changed); err != nil {
		return err
	}
	r := &fluidRuntime{world: w, settings: w.fluidSettings()}
	return r.NotifyButtonNeighbors(worldgen.FeaturePosition(p), worldgen.SurfaceBlockState{Name: state.Name, Properties: state.Properties})
}

// ButtonBlock.updateNeighbours notifies around both the button and its support.
func (r *fluidRuntime) NotifyButtonNeighbors(p worldgen.FeaturePosition, state worldgen.SurfaceBlockState) error {
	face := 0
	switch state.Properties["face"] {
	case "ceiling":
		face = 1
	case "wall":
		face = oppositeFace(FacingFace(state.Properties["facing"]))
	}
	for _, center := range []worldgen.FeaturePosition{p, fluidAdjacent(p, face)} {
		if err := r.enqueue(&fluidNeighborUpdate{position: center, multi: true, loadedOnly: true}); err != nil {
			return err
		}
	}
	return nil
}

func (w *World) editBlock(msg setBlockMsg) ([]BlockPosition, error) {
	msg.result = make(chan error, 1)
	msg.changed = make(chan []BlockPosition, 1)
	if err := w.post(context.Background(), msg.x>>4, msg.z>>4, msg); err != nil {
		return nil, err
	}
	select {
	case err := <-msg.result:
		if err != nil {
			return nil, err
		}
		return <-msg.changed, nil
	case <-w.done:
		return nil, ErrWorldClosed
	}
}

// Cold terrain can take several seconds to prepare. Keep the world clock
// free while it loads, then compare and edit the current state in the actor.
func (w *World) editAndUpdateNeighbors(msg setBlockMsg) error {
	msg.loadedOnly = true
	for {
		w.blockUpdateMu.Lock()
		changed, err := w.editBlock(msg)
		if !errors.Is(err, ErrChunkNotLoaded) {
			if err == nil {
				err = w.updateBlockNeighbors(changed)
			}
			w.blockUpdateMu.Unlock()
			return err
		}
		w.blockUpdateMu.Unlock()
		if err := w.ensureChunk(context.Background(), msg.x>>4, msg.z>>4); err != nil {
			return err
		}
	}
}

// Only loaded blocks participate. Propagation runs outside the actor, so
// attachments across region boundaries cannot deadlock another region.
func (w *World) updateBlockNeighbors(changed []BlockPosition) error {
	queue := append([]BlockPosition(nil), changed...)
	runtime := &fluidRuntime{world: w, settings: w.fluidSettings()}
	for step := 0; len(queue) > 0; step++ {
		if step >= 4096 {
			return errors.New("block neighbor update limit exceeded")
		}
		p := queue[0]
		queue = queue[1:]
		var changedState worldgen.SurfaceBlockState
		candidates := []BlockPosition{p}
		for _, face := range fluidShapeOrder {
			candidates = append(candidates, neighbor(p, face))
		}
		for index, target := range candidates {
			if target.Y < w.MinBuildHeight() || target.Y >= w.MaxBuildHeight() {
				continue
			}
			states, err := w.ReadLoadedBlocks([]BlockPosition{target})
			if errors.Is(err, ErrChunkNotLoaded) {
				continue
			}
			if err != nil {
				return err
			}
			state, err := BlockStateByID(states[0])
			if err != nil {
				return err
			}
			updated := states[0]
			position := worldgen.FeaturePosition(target)
			s := worldgen.SurfaceBlockState{Name: state.Name, Properties: state.Properties}
			if target == p {
				changedState = s
			}
			if liquidBlock(s) {
				err = worldgen.NotifyLiquidBlock(runtime, position, s, runtime.settings)
				if errors.Is(err, ErrChunkNotLoaded) {
					// Mixing may inspect unloaded neighbors. Keep the liquid due
					// for simulation once its neighborhood is ready.
					err = runtime.ScheduleFluidUpdate(position)
				}
				if err != nil {
					return err
				}
				continue
			}
			if target != p && worldgen.IsLeafBlock(state.Name) {
				// The native shape callback schedules water (if present) before
				// the leaf distance check. It does not change the leaf inline.
				if err := worldgen.UpdateLeafNeighbor(runtime, position, s, changedState); err != nil {
					return err
				}
				continue
			}
			if s.Properties["waterlogged"] == "true" {
				if err := runtime.ScheduleFluidUpdate(position); err != nil {
					return err
				}
			}
			if worldgen.IsFallingBlock(state.Name) || worldgen.IsBrushableBlock(state.Name) ||
				state.Name == "minecraft:bubble_column" || state.Name == "minecraft:magma_block" || state.Name == "minecraft:soul_sand" {
				if target == p {
					err = worldgen.NotifyFallingBlockPlacement(runtime, position, s)
					if err == nil {
						err = worldgen.NotifyBrushableBlockPlacement(runtime, position, s)
					}
					if err == nil {
						err = worldgen.NotifyBubbleBlockPlacement(runtime, position, s)
					}
				} else {
					face := oppositeFace(fluidShapeOrder[index-1])
					var after worldgen.SurfaceBlockState
					after, err = worldgen.UpdateFluidNeighborShape(runtime, position, s, face, changedState)
					if err == nil {
						updated, err = BlockStateID(after.Name, after.Properties)
					}
				}
			} else {
				updated, err = w.updatedNeighborState(target, states[0], state)
			}
			if errors.Is(err, ErrChunkNotLoaded) {
				continue
			}
			if err != nil {
				return err
			}
			if updated == states[0] {
				continue
			}
			changes, err := w.editBlock(setBlockMsg{x: target.X, y: target.Y, z: target.Z, state: updated, expected: &states[0]})
			if err != nil {
				return err
			}
			queue = append(queue, changes...)
		}
	}
	return nil
}

func (w *World) loadedState(p BlockPosition) (BlockState, uint32, error) {
	ids, err := w.ReadLoadedBlocks([]BlockPosition{p})
	if err != nil {
		return BlockState{}, 0, err
	}
	state, err := BlockStateByID(ids[0])
	return state, ids[0], err
}

func (w *World) CanSupportBlock(p BlockPosition, id uint32) (bool, error) {
	state, err := BlockStateByID(id)
	if err != nil {
		return false, err
	}
	face := -1
	center := false
	switch {
	case state.Name == "minecraft:torch" || state.Name == "minecraft:soul_torch" || state.Name == "minecraft:redstone_torch":
		face = 0
		center = true
	case state.Name == "minecraft:wall_torch" || state.Name == "minecraft:soul_wall_torch" || state.Name == "minecraft:redstone_wall_torch":
		face = oppositeFace(FacingFace(state.Properties["facing"]))
	case state.Name == "minecraft:lantern" || state.Name == "minecraft:soul_lantern":
		face = 0
		center = true
		if state.Properties["hanging"] == "true" {
			face = 1
		}
	case strings.HasSuffix(state.Name, "_button") || state.Name == "minecraft:lever":
		switch state.Properties["face"] {
		case "floor":
			face = 0
		case "ceiling":
			face = 1
		case "wall":
			face = oppositeFace(FacingFace(state.Properties["facing"]))
		}
	case IsDoor(state.Name):
		if state.Properties["half"] == "upper" {
			below, _, err := w.loadedState(neighbor(p, 0))
			return below.Name == state.Name && below.Properties["half"] == "lower", err
		}
		face = 0
	}
	if face < 0 || face >= 6 {
		return true, nil
	}
	supportState, below, err := w.loadedState(neighbor(p, face))
	if err != nil {
		return false, err
	}
	rules, err := blockrules.Lookup(below)
	if err != nil {
		return false, err
	}
	if center && face == 0 {
		return rules.TopCenter, nil
	}
	if center && face == 1 {
		// Block.canSupportCenter rejects unstable_bottom_center (fence gates).
		return rules.BottomCenter && !strings.HasSuffix(supportState.Name, "_fence_gate"), nil
	}
	return rules.FaceSturdy[oppositeFace(face)], nil
}

func (w *World) updatedNeighborState(p BlockPosition, id uint32, state BlockState) (uint32, error) {
	survives, err := w.CanSupportBlock(p, id)
	if err != nil {
		return id, err
	}
	if !survives {
		return StateAir, nil
	}
	if IsDoor(state.Name) {
		face := 1
		expectedHalf := "upper"
		if state.Properties["half"] == "upper" {
			face = 0
			expectedHalf = "lower"
		}
		other, _, err := w.loadedState(neighbor(p, face))
		if err != nil {
			return id, err
		}
		if other.Name != state.Name || other.Properties["half"] != expectedHalf {
			return StateAir, nil
		}
		if state.Properties["half"] == "upper" {
			for _, key := range []string{"facing", "hinge", "open", "powered"} {
				state.Properties[key] = other.Properties[key]
			}
			return BlockStateID(state.Name, state.Properties)
		}
	}
	if strings.HasSuffix(state.Name, "_fence") || state.Name == "minecraft:iron_bars" || strings.HasSuffix(state.Name, "glass_pane") {
		for _, facing := range []string{"north", "south", "west", "east"} {
			face := FacingFace(facing)
			other, otherID, err := w.loadedState(neighbor(p, face))
			if err != nil {
				return id, err
			}
			rules, err := blockrules.Lookup(otherID)
			if err != nil {
				return id, err
			}
			// Leaves, barriers, gourds and shulker boxes must not create a
			// connection merely because the face is sturdy. Bootstrap probes
			// do not bind tags, so include the shulker_boxes tag by name here.
			exception := rules.ConnectionException || strings.HasSuffix(other.Name, "shulker_box")
			connect := rules.FaceSturdy[oppositeFace(face)] && !exception
			if strings.HasSuffix(state.Name, "_fence") {
				wood := state.Name != "minecraft:nether_brick_fence"
				otherWood := other.Name != "minecraft:nether_brick_fence"
				connect = connect || strings.HasSuffix(other.Name, "_fence") && wood == otherWood
				if strings.HasSuffix(other.Name, "_fence_gate") {
					gateFace := FacingFace(other.Properties["facing"])
					connect = connect || face >= 2 && (face/2) != (gateFace/2)
				}
			} else {
				connect = connect || other.Name == "minecraft:iron_bars" || strings.HasSuffix(other.Name, "glass_pane")
			}
			state.Properties[facing] = "false"
			if connect {
				state.Properties[facing] = "true"
			}
		}
		return BlockStateID(state.Name, state.Properties)
	}
	return id, nil
}

// UpdateDoor commits both halves in the same column write, including collision
// state and client updates. A click on either half changes the same lower door.
func (w *World) UpdateDoor(p BlockPosition, expected uint32, open bool) error {
	w.blockUpdateMu.Lock()
	defer w.blockUpdateMu.Unlock()
	state, err := BlockStateByID(expected)
	if err != nil || !IsDoor(state.Name) {
		return errors.New("not a door")
	}
	if state.Properties["half"] == "upper" {
		p = neighbor(p, 0)
	}
	lower, lowerID, err := w.loadedState(p)
	if err != nil {
		return err
	}
	upper, upperID, err := w.loadedState(neighbor(p, 1))
	if err != nil {
		return err
	}
	if lower.Name != state.Name || upper.Name != state.Name || lower.Properties["half"] != "lower" || upper.Properties["half"] != "upper" {
		return ErrBlockChanged
	}
	lower.Properties["open"] = "false"
	if open {
		lower.Properties["open"] = "true"
	}
	upper.Properties["open"] = lower.Properties["open"]
	newLower, err := BlockStateID(lower.Name, lower.Properties)
	if err != nil {
		return err
	}
	newUpper, err := BlockStateID(upper.Name, upper.Properties)
	if err != nil {
		return err
	}
	changed, err := w.editBlock(setBlockMsg{x: p.X, y: p.Y, z: p.Z, state: newLower, expected: &lowerID, upper: &newUpper, upperExpected: &upperID})
	if err != nil {
		return err
	}
	return w.updateBlockNeighbors(changed)
}
