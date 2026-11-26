package world

import (
	"atlas/internal/worldgen"
	_ "embed"
	"errors"
	"fmt"
	"strconv"
)

// Registry flags captured from the Java server: block ticks, fluid ticks, nonempty fluid,
// water-tagged fluid, and membership in #maintains_farmland.
//
//go:embed random_tick_states_1.21.1.bin
var randomStateFlags string

const (
	tickLeaf     byte = 1
	tickWheat    byte = 2
	tickFarmland byte = 3
)

var randomHandlers = func() [MaxBlockState + 1]byte {
	if len(randomStateFlags) != MaxBlockState+1 {
		panic("incomplete random tick registry")
	}
	var table [MaxBlockState + 1]byte
	for id := range len(randomStateFlags) {
		if randomStateFlags[id]&1 == 0 {
			continue
		}
		state, err := BlockStateByID(uint32(id))
		if err != nil {
			panic(err)
		}
		switch {
		case worldgen.IsLeafBlock(state.Name):
			table[id] = tickLeaf
		case state.Name == "minecraft:farmland":
			table[id] = tickFarmland
		case state.Name == "minecraft:wheat":
			table[id] = tickWheat
		}
	}
	return table
}()

func supportedTickCount(id uint32) int16 {
	if randomHandlers[id] != 0 {
		return 1
	}
	return 0
}

// The owning region is the only writer. Readers need no region messages to skip
// empty sections or consume positions in sections with no ported callbacks.
func (h *chunkScheduledTicks) publishSection(i int, s *Section) {
	bit := uint64(1) << i
	before := h.randomSections.Load()
	after := before &^ (bit | bit<<32)
	if s.tickingBlocks > 0 || s.tickingFluids > 0 {
		after |= bit
	}
	if s.supportedTicks > 0 {
		after |= bit << 32
	}
	if before != after {
		h.randomSections.Store(after)
	}
}

func (w *World) gameRule(name, fallback string) string {
	w = w.clockOwner()
	w.stateMu.RLock()
	override, ok := w.ruleOverrides[name]
	w.stateMu.RUnlock()
	if ok {
		return override
	}
	if w.store != nil {
		v := w.store.vanilla
		v.mu.Lock()
		defer v.mu.Unlock()
		root, _ := v.root.Object()
		data, _ := root.Object("Data")
		rules, _ := data.Object("GameRules")
		if value, ok := rules.Text(name); ok {
			return value
		}
	}
	return fallback
}

func (r *fluidRuntime) tickRandomChunks(chunks TickChunks, elapsed int64) error {
	w := r.world
	if err := w.ensureTickRandom(); err != nil {
		return err
	}
	speed, err := strconv.ParseInt(w.gameRule("randomTickSpeed", "3"), 10, 32)
	if err != nil {
		return fmt.Errorf("randomTickSpeed: %w", err)
	}
	o := w.scheduledTicks
	o.mu.Lock()
	var holders []*chunkScheduledTicks
	for _, h := range o.loadOrder {
		if (chunks.Blocks == nil || chunks.Blocks(h.x, h.z)) && o.ready(h) && (h.active || chunks.Blocks == nil) {
			holders = append(holders, h)
		}
	}
	o.mu.Unlock()
	for i := len(holders); i > 1; i-- {
		j := w.tickRandom.NextIntBound(int32(i))
		holders[i-1], holders[j] = holders[j], holders[i-1]
	}
	var failures []error
	for _, h := range holders {
		if chunks.Random(h.x, h.z) {
			o.mu.Lock()
			h.inhabitedTime += elapsed
			h.revision++
			o.mu.Unlock()
			if err := r.randomTickChunk(h, int32(speed), nil); err != nil {
				failures = append(failures, fmt.Errorf("chunk %d,%d: %w", h.x, h.z, err))
			}
		}
	}
	return errors.Join(failures...)
}

func (w *World) nextBlockPosition(x, y, z int32) worldgen.FeaturePosition {
	w.randomPosition = w.randomPosition*3 + 1013904223
	bits := w.randomPosition >> 2
	return worldgen.FeaturePosition{X: x + int32(bits&15), Y: y + int32(bits>>16&15), Z: z + int32(bits>>8&15)}
}

func (r *fluidRuntime) randomTickChunk(h *chunkScheduledTicks, speed int32, observe func(worldgen.FeaturePosition, bool)) error {
	w := r.world
	x, z := h.x*16, h.z*16
	levels := w.WeatherLevels()
	if w.HasSkyLight() && levels.IsRaining() && float64(levels.Rain*levels.Thunder) > 0.9 && w.tickRandom.NextIntBound(100000) == 0 {
		if err := r.naturalLightning(w.nextBlockPosition(x, 0, z)); err != nil {
			return err
		}
	}
	// Precipitation precedes section sampling, including in clear weather.
	for i := int32(0); i < speed; i++ {
		if w.tickRandom.NextIntBound(48) == 0 {
			p := w.nextBlockPosition(x, 0, z)
			if observe != nil {
				observe(p, true)
			}
			if err := r.tickPrecipitation(p, levels.IsRaining()); err != nil {
				return err
			}
		}
	}
	// Check eligibility once on entry, then finish all draws even if the last
	// ticking state disappears. Look up each selected cell after preceding edits.
	for y := w.MinBuildHeight(); y < w.MaxBuildHeight(); y += 16 {
		bit := uint64(1) << ((y - w.MinBuildHeight()) >> 4)
		if h.randomSections.Load()&bit == 0 {
			continue
		}
		for i := int32(0); i < speed; i++ {
			p := w.nextBlockPosition(x, y, z)
			if observe != nil {
				observe(p, false)
			}
			if h.randomSections.Load()&(bit<<32) == 0 {
				continue
			}
			ids, err := w.ReadLoadedBlocks([]BlockPosition{BlockPosition(p)})
			if err != nil {
				return err
			}
			handler := randomHandlers[ids[0]]
			if handler != 0 {
				s, err := BlockStateByID(ids[0])
				if err != nil {
					return err
				}
				state := worldgen.SurfaceBlockState{Name: s.Name, Properties: s.Properties}
				switch handler {
				case tickLeaf:
					err = r.decayLeaf(p, state)
				case tickFarmland:
					err = r.tickFarmland(p, state)
				case tickWheat:
					err = r.growWheat(p, state)
				}
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (r *fluidRuntime) growWheat(p worldgen.FeaturePosition, s worldgen.SurfaceBlockState) error {
	light, err := r.FeatureRawBrightness(p, 0)
	if err != nil || light < 9 {
		return err
	}
	cells := make([]BlockPosition, 0, 18)
	for dy := int32(-1); dy <= 0; dy++ {
		for dz := int32(-1); dz <= 1; dz++ {
			for dx := int32(-1); dx <= 1; dx++ {
				cells = append(cells, BlockPosition{p.X + dx, p.Y + dy, p.Z + dz})
			}
		}
	}
	ids, err := r.world.ReadLoadedBlocks(cells)
	if err != nil {
		return err
	}
	growth := float32(1)
	var adjacent [9]bool
	for i := range 9 {
		soil, err := BlockStateByID(ids[i])
		if err != nil {
			return err
		}
		if soil.Name == "minecraft:farmland" {
			amount := float32(1)
			if soil.Properties["moisture"] != "0" {
				amount = 3
			}
			if i != 4 {
				amount /= 4
			}
			growth += amount
		}
		neighbor, err := BlockStateByID(ids[i+9])
		if err != nil {
			return err
		}
		adjacent[i] = neighbor.Name == s.Name
	}
	if ((adjacent[3] || adjacent[5]) && (adjacent[1] || adjacent[7])) || adjacent[0] || adjacent[2] || adjacent[6] || adjacent[8] {
		growth /= 2
	}
	if r.world.tickRandom.NextIntBound(int32(25/growth)+1) != 0 {
		return nil
	}
	age, err := strconv.Atoi(s.Properties["age"])
	if err != nil {
		return err
	}
	s.Properties["age"] = strconv.Itoa(age + 1)
	return r.SetFluidBlock(p, s, 2)
}
