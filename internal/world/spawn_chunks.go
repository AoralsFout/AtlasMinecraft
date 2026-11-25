package world

import (
	"atlas/internal/nbt"
	"errors"
	"fmt"
	"maps"
	"strconv"
)

type spawnChunkTicket struct {
	center ChunkPosition
	radius int32 // gamerule value, zero disables the START ticket
}

func (v *VanillaSave) savedSpawnRadius() (int32, error) {
	if v.dimension.name != "minecraft:overworld" {
		return 0, nil
	}
	root, _ := v.root.Object()
	level, _ := root.Object("Data")
	rules, _ := level.Object("GameRules")
	value, ok := rules.Text("spawnChunkRadius")
	if !ok {
		return 2, nil
	}
	radius, err := strconv.ParseInt(value, 10, 32)
	if err != nil || radius < 0 || radius > 32 {
		return 0, fmt.Errorf("invalid saved spawnChunkRadius %q", value)
	}
	return int32(radius), nil
}

func (w *World) spawnTicketLocked() spawnChunkTicket {
	if w.spawnRadius == 0 {
		return spawnChunkTicket{}
	}
	x, z := w.SpawnXZ()
	return spawnChunkTicket{center: ChunkPosition{x >> 4, z >> 4}, radius: w.spawnRadius}
}

func (w *World) refreshSpawnTicketsLocked() {
	if tickets := w.simulation.Load(); tickets != nil && tickets.spawn != w.spawnTicketLocked() {
		w.publishSimulationTickets(tickets.centers, tickets.distance)
	}
}

func (w *World) SpawnChunkRadius() int32 {
	w.blockUpdateMu.Lock()
	defer w.blockUpdateMu.Unlock()
	return w.spawnRadius
}

func (w *World) SetSpawnChunkRadius(radius int32) error {
	if radius < 0 || radius > 32 {
		return errors.New("spawnChunkRadius must be between 0 and 32")
	}
	if w.store == nil || w.DimensionName() != "minecraft:overworld" {
		return errors.New("spawnChunkRadius requires an Overworld save")
	}
	w.blockUpdateMu.Lock()
	defer w.blockUpdateMu.Unlock()
	w.saveMu.Lock()
	defer w.saveMu.Unlock()
	select {
	case <-w.done:
		return ErrWorldClosed
	default:
	}
	if radius == w.spawnRadius {
		return nil
	}
	v := w.store.vanilla
	if err := v.saveSpawnRadius(radius); err != nil {
		return err
	}
	w.spawnRadius = radius
	w.refreshSpawnTicketsLocked()
	return nil
}

func (v *VanillaSave) saveSpawnRadius(radius int32) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	root, _ := v.root.Object()
	old, _ := root.Object("Data")
	level := maps.Clone(old)
	oldRules, _ := level.Object("GameRules")
	rules := maps.Clone(oldRules)
	if rules == nil {
		rules = nbt.Object{}
	}
	rules["spawnChunkRadius"] = nbt.Tag{Type: nbt.String, Value: strconv.FormatInt(int64(radius), 10)}
	level["GameRules"] = nbt.Tag{Type: nbt.Compound, Value: rules}
	return v.commitLevelLocked(level)
}
