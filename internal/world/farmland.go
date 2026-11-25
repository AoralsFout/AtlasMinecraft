package world

import (
	"atlas/internal/registry"
	"atlas/internal/worldgen"
	"strconv"
)

// FarmBlock.isNearWater uses both horizontal diagonals and the layer above.
// Read the complete neighborhood in actor batches, including across chunk edges.
func (r *fluidRuntime) tickFarmland(p worldgen.FeaturePosition, s worldgen.SurfaceBlockState) error {
	const area = 9 * 9
	var positions [2 * area]BlockPosition
	i := 0
	for y := p.Y; y <= p.Y+1; y++ {
		for z := p.Z - 4; z <= p.Z+4; z++ {
			for x := p.X - 4; x <= p.X+4; x++ {
				positions[i] = BlockPosition{x, y, z}
				i++
			}
		}
	}
	states, err := r.world.ReadLoadedBlocks(positions[:])
	if err != nil {
		return err
	}
	wet := false
	for _, id := range states {
		if randomStateFlags[id]&8 != 0 {
			wet = true
			break
		}
	}
	if !wet {
		wet, err = r.rainingAt(fluidAdjacent(p, 1))
		if err != nil {
			return err
		}
	}
	moisture, err := strconv.Atoi(s.Properties["moisture"])
	if err != nil {
		return err
	}
	if wet {
		if moisture == 7 {
			return nil
		}
		moisture = 7
	} else if moisture > 0 {
		moisture--
	} else {
		const above = area + area/2 // center of upper layer
		if randomStateFlags[states[above]]&16 != 0 {
			return nil
		}
		return worldgen.TurnFarmlandToDirt(r, p)
	}
	s.Properties["moisture"] = strconv.Itoa(moisture)
	return r.SetFluidBlock(p, s, 2)
}

func (r *fluidRuntime) rainingAt(p worldgen.FeaturePosition) (bool, error) {
	w := r.world
	if !w.HasSkyLight() || !w.WeatherLevels().IsRaining() {
		return false, nil
	}
	sky, _, err := r.rawLightAt(p)
	if err != nil || sky < 15 {
		return false, err
	}
	var sheltered bool
	err = w.fallingEntityOperation(p.X>>4, p.Z>>4, func(_ *Region, c *Chunk) error {
		x, z := int(p.X&15), int(p.Z&15)
		for y := c.Height(x, z) - 1; y >= p.Y; y-- {
			if stateFlag(motionFlags, c.GetBlock(x, z, y)) {
				sheltered = true
				break
			}
		}
		return nil
	})
	if err != nil || sheltered {
		return false, err
	}
	biome, err := w.biomeAt(p)
	if err != nil {
		return false, err
	}
	return worldgen.BiomeRainsAt(biome, p.X, p.Y, p.Z)
}

func (w *World) biomeAt(p worldgen.FeaturePosition) (string, error) {
	qx, qy, qz := worldgen.BiomeQuartAtBlock(w.seed, p.X, p.Y, p.Z)
	var id int32
	err := w.fallingEntityOperation(qx>>2, qz>>2, func(_ *Region, c *Chunk) error {
		y := min(max(qy, c.minY>>2), (c.maxY-1)>>2)
		id = c.biomes[(y-(c.minY>>2))>>2][(y&3)<<4|(qz&3)<<2|(qx&3)]
		return nil
	})
	if err != nil {
		return "", err
	}
	return registry.BiomeName(id)
}
