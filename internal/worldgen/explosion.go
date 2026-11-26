package worldgen

import "math"

// ExplosionBlocks follows Explosion.explode's shell rays, including the air
// positions that take part in the later shuffle. A negative resistance denotes air.
func ExplosionBlocks(center [3]float64, power float32, rng *LegacyRandom, sample func(FeaturePosition) (resistance float32, inWorld bool, err error)) ([]FeaturePosition, error) {
	var hit javaPositionSet
	component := func(i int) float64 { return float64(float32(i)/15*2 - 1) }
	for i := 0; i < 16; i++ {
		for j := 0; j < 16; j++ {
			for k := 0; k < 16; k++ {
				if i != 0 && i != 15 && j != 0 && j != 15 && k != 0 && k != 15 {
					continue
				}
				dx, dy, dz := component(i), component(j), component(k)
				length := math.Sqrt(dx*dx + dy*dy + dz*dz)
				const step = float64(float32(.3))
				dx, dy, dz = dx/length*step, dy/length*step, dz/length*step
				x, y, z := center[0], center[1], center[2]
				for energy := power * (.7 + float32(rng.NextFloat()*.6)); energy > 0; energy -= .22500001 {
					p := FeaturePosition{X: int32(math.Floor(x)), Y: int32(math.Floor(y)), Z: int32(math.Floor(z))}
					r, inside, err := sample(p)
					if err != nil {
						return nil, err
					}
					if !inside {
						break
					}
					if r >= 0 {
						energy -= float32((r + .3) * .3)
					}
					if energy > 0 {
						hit.add(p)
					}
					x, y, z = x+dx, y+dy, z+dz
				}
			}
		}
	}
	return hit.positions(), nil
}
