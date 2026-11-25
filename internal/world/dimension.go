package world

import (
	"atlas/internal/nbt"
	"fmt"
)

type dimensionSettings struct {
	name         string
	generator    string
	regionSubdir string
	minY, maxY   int32
	hasSky       bool
}

// ShareLevelData attaches a dimension before it joins the server tick loop.
func (w *World) ShareLevelData(overworld *World) {
	w.blockUpdateMu.Lock()
	defer w.blockUpdateMu.Unlock()
	w.randomSequences = overworld.randomSequences
	w.clockSource.Store(overworld)
	age, _ := overworld.Time()
	w.scheduledTicks.setTime(age)
}

func builtinDimension(name string) (dimensionSettings, error) {
	switch name {
	case "overworld", "minecraft:overworld":
		return dimensionSettings{name: "minecraft:overworld", generator: "overworld", minY: MinY, maxY: MaxY, hasSky: true}, nil
	case "nether", "the_nether", "minecraft:the_nether":
		return dimensionSettings{name: "minecraft:the_nether", generator: "nether", regionSubdir: "DIM-1", minY: 0, maxY: 256}, nil
	case "end", "the_end", "minecraft:the_end":
		return dimensionSettings{name: "minecraft:the_end", generator: "end", regionSubdir: "DIM1", minY: 0, maxY: 256}, nil
	default:
		return dimensionSettings{}, fmt.Errorf("unsupported world dimension %q", name)
	}
}

// OpenDimension reads the root world's shared level.dat and opens the vanilla
// region directory for one built-in dimension. Closing it saves its chunks;
// only the Overworld owner may rewrite the shared world metadata.
func OpenDimension(hub HubView, genWorkers int, dir, dimension string) (*World, error) {
	s, err := OpenStoreDimension(dir, dimension)
	if err != nil {
		return nil, err
	}
	m, err := s.LoadMeta()
	if err != nil {
		return nil, err
	}
	forced, err := readForcedChunks(s.vanilla)
	if err != nil {
		return nil, err
	}
	radius, err := s.vanilla.savedSpawnRadius()
	if err != nil {
		return nil, err
	}
	w := newWorld(hub, genWorkers, s, m)
	w.blockUpdateMu.Lock()
	w.forced = forced
	w.spawnRadius = radius
	// Opening a save for offline reads does not start its game clock.
	// Restore explicit forced loads immediately; the first TickPlayers also
	// starts spawn loading for worlds without forced chunks.
	if len(forced.positions) != 0 {
		w.publishSimulationTickets(nil, 0)
	}
	w.blockUpdateMu.Unlock()
	return w, nil
}

func OpenStoreDimension(dir, dimension string) (*Store, error) {
	v, err := openVanillaDimension(dir, dimension)
	if err != nil {
		return nil, err
	}
	return &Store{vanilla: v}, nil
}

func (w *World) DimensionName() string {
	if w.dimension.name == "" {
		return "minecraft:overworld"
	}
	return w.dimension.name
}
func (w *World) MinBuildHeight() int32 {
	if w.dimension.maxY == 0 {
		return MinY
	}
	return w.dimension.minY
}
func (w *World) MaxBuildHeight() int32 {
	if w.dimension.maxY == 0 {
		return MaxY
	}
	return w.dimension.maxY
}
func (w *World) HasSkyLight() bool { return w.dimension.maxY == 0 || w.dimension.hasSky }

func supportsDimensionGenerator(gen nbt.Object, dimension dimensionSettings) bool {
	settings, _ := gen.Text("settings")
	if settings != "minecraft:"+dimension.generator {
		return false
	}
	source, ok := gen.Object("biome_source")
	if !ok {
		return false
	}
	kind, _ := source.Text("type")
	if dimension.generator == "end" {
		return kind == "minecraft:the_end"
	}
	preset, _ := source.Text("preset")
	return kind == "minecraft:multi_noise" && preset == "minecraft:nether"
}
