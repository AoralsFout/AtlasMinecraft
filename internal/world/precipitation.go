package world

import (
	"atlas/internal/worldgen"
	"strconv"
)

// The biome is chosen at the first position above MOTION_BLOCKING. Vanilla
// tests that same biome's temperature at two different heights in the column.
func (r *fluidRuntime) tickPrecipitation(p worldgen.FeaturePosition, raining bool) error {
	w := r.world
	var groundID, surfaceID uint32
	err := w.fallingEntityOperation(p.X>>4, p.Z>>4, func(_ *Region, c *Chunk) error {
		x, z := int(p.X&15), int(p.Z&15)
		p.Y = c.Height(x, z)
		for p.Y > c.minY {
			id := c.GetBlock(x, z, p.Y-1)
			if stateFlag(motionFlags, id) {
				groundID = id
				break
			}
			p.Y--
		}
		if p.Y < c.maxY {
			surfaceID = c.GetBlock(x, z, p.Y)
		}
		return nil
	})
	if err != nil {
		return err
	}
	ground := worldgen.FeaturePosition{X: p.X, Y: p.Y - 1, Z: p.Z}
	if !r.InWorld(ground) {
		return nil
	}
	// Do not look up neighboring biome chunks for a dry column with no water.
	freeze := groundID == StateWater
	snowCap := int32(0)
	if raining && r.InWorld(p) {
		snowCap = min(int32(8), w.SnowAccumulationHeight())
	}
	if !freeze && snowCap <= 0 {
		return nil
	}
	biome, err := w.biomeAt(p)
	if err != nil {
		return err
	}
	if freeze {
		if err := r.freezeSurfaceWater(biome, ground); err != nil {
			return err
		}
	}
	if snowCap <= 0 {
		return nil
	}
	surface, err := BlockStateByID(surfaceID)
	if err != nil {
		return err
	}
	layers := 0
	if surface.Name == "minecraft:snow" {
		layers, err = strconv.Atoi(surface.Properties["layers"])
		if err != nil {
			return err
		}
	} else if !isAir(surfaceID) {
		return nil
	}
	if int32(layers) >= snowCap {
		return nil
	}
	eligible, err := r.coldAndDark(biome, p)
	if err != nil || !eligible {
		return err
	}
	below, err := r.BlockAt(ground)
	if err != nil {
		return err
	}
	supported, err := worldgen.SnowSupported(below)
	if err != nil || !supported {
		return err
	}
	return r.SetFluidBlock(p, worldgen.SurfaceBlockState{Name: "minecraft:snow", Properties: map[string]string{"layers": strconv.Itoa(layers + 1)}}, 3)
}

func (r *fluidRuntime) freezeSurfaceWater(biome string, p worldgen.FeaturePosition) error {
	eligible, err := r.coldAndDark(biome, p)
	if err != nil || !eligible {
		return err
	}
	neighbors := []BlockPosition{
		{p.X - 1, p.Y, p.Z}, {p.X + 1, p.Y, p.Z},
		{p.X, p.Y, p.Z - 1}, {p.X, p.Y, p.Z + 1},
	}
	ids, err := r.world.ReadLoadedBlocks(neighbors)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if randomStateFlags[id]&8 == 0 {
			return r.SetFluidBlock(p, worldgen.SurfaceBlockState{Name: "minecraft:ice"}, 3)
		}
	}
	return nil
}

func (r *fluidRuntime) coldAndDark(biome string, p worldgen.FeaturePosition) (bool, error) {
	cold, err := worldgen.BiomeColdAt(biome, p.X, p.Y, p.Z)
	if err != nil || !cold {
		return false, err
	}
	_, block, err := r.rawLightAt(p)
	return block < 10, err
}
