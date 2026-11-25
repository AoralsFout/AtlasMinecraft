package world

import (
	"atlas/internal/worldgen"
	"errors"
	"strconv"
)

// Only the timers and flags are serialized. Level.prepareWeather reconstructs
// the intensities from the flags when opening a dimension.
type weatherTimers struct {
	ClearTime, RainTime, ThunderTime int32
	Raining, Thundering              bool
}

type WeatherLevels struct{ Rain, Thunder float32 }

// WeatherPublisher receives the local intensity changes and the global rain transition.
type WeatherPublisher func(previous, current WeatherLevels, changedRain bool)

func (l WeatherLevels) IsRaining() bool { return float64(l.Rain) > 0.2 }

func (w *World) WeatherLevels() WeatherLevels {
	w.stateMu.RLock()
	defer w.stateMu.RUnlock()
	return w.levels
}

func (w *World) Weather() string {
	w = w.clockOwner()
	w.stateMu.RLock()
	defer w.stateMu.RUnlock()
	switch {
	case !w.meta.Raining:
		return "clear"
	case w.meta.Thundering:
		return "thunder"
	default:
		return "rain"
	}
}

func (w *World) WeatherCycle() bool {
	w = w.clockOwner()
	w.stateMu.RLock()
	defer w.stateMu.RUnlock()
	return !w.weatherStopped
}

func (w *World) SetWeatherCycle(on bool) error {
	return w.setCycleRule("doWeatherCycle", strconv.FormatBool(on), func(owner *World) { owner.weatherStopped = !on })
}

func (w *World) SnowAccumulationHeight() int32 {
	w = w.clockOwner()
	w.stateMu.RLock()
	defer w.stateMu.RUnlock()
	return w.snowHeight
}

func (w *World) SetSnowAccumulationHeight(height int32) error {
	return w.setCycleRule("snowAccumulationHeight", strconv.FormatInt(int64(height), 10), func(owner *World) { owner.snowHeight = height })
}

func weatherDuration(rng *worldgen.LegacyRandom, active, thunder bool) int32 {
	low, high := int32(12000), int32(180000)
	if active {
		high = 24000
		if thunder {
			low, high = 3600, 15600
		}
	}
	return low + rng.NextIntBound(high-low+1)
}

func (w *World) SetWeather(kind string) error { return w.SetWeatherDuration(kind, 0) }

// A zero duration selects the command's original duration provider.
func (w *World) SetWeatherDuration(kind string, ticks int32) error {
	if kind != "clear" && kind != "rain" && kind != "thunder" {
		return errors.New("weather must be clear, rain, or thunder")
	}
	if ticks < 0 {
		return errors.New("negative weather duration")
	}
	w = w.clockOwner()
	w.blockUpdateMu.Lock()
	defer w.blockUpdateMu.Unlock()
	if ticks == 0 {
		if err := w.ensureTickRandom(); err != nil {
			return err
		}
		ticks = weatherDuration(w.tickRandom, kind != "clear", kind == "thunder")
	}
	m := weatherTimers{ClearTime: ticks}
	if kind != "clear" {
		m = weatherTimers{RainTime: ticks, ThunderTime: ticks, Raining: true, Thundering: kind == "thunder"}
	}
	w.stateMu.Lock()
	w.meta.weatherTimers = m
	w.stateMu.Unlock()
	return nil
}

func advanceWeatherTimer(timer int32, active, thunder bool, rng *worldgen.LegacyRandom) (int32, bool) {
	if timer <= 0 {
		return weatherDuration(rng, active, thunder), active
	}
	timer--
	if timer == 0 {
		active = !active
	}
	return timer, active
}

func forcedClearTimer(active bool) int32 {
	if active {
		return 0
	}
	return 1
}

func advanceIntensity(value float32, active bool) float32 {
	if active {
		return min(float32(1), value+0.01)
	}
	return max(float32(0), value-0.01)
}

func (w *World) advanceWeather(publish WeatherPublisher) error {
	if w.HasSkyLight() {
		if err := w.ensureTickRandom(); err != nil {
			return err
		}
	}
	w.stateMu.Lock()
	before := w.levels
	previous := before
	if w.HasSkyLight() {
		m := &w.meta
		if !w.weatherStopped {
			if m.ClearTime > 0 {
				m.ClearTime--
				m.ThunderTime, m.RainTime = forcedClearTimer(m.Thundering), forcedClearTimer(m.Raining)
				m.Thundering, m.Raining = false, false
			} else {
				m.ThunderTime, m.Thundering = advanceWeatherTimer(m.ThunderTime, m.Thundering, true, w.tickRandom)
				m.RainTime, m.Raining = advanceWeatherTimer(m.RainTime, m.Raining, false, w.tickRandom)
			}
		}
		w.levels.Thunder = advanceIntensity(w.levels.Thunder, m.Thundering)
		w.levels.Rain = advanceIntensity(w.levels.Rain, m.Raining)
	} else {
		// Without skylight vanilla never copies the startup intensity to its previous field.
		previous = WeatherLevels{}
	}
	current := w.levels
	w.stateMu.Unlock()
	if publish != nil && previous != current {
		publish(previous, current, before.IsRaining() != current.IsRaining())
	}
	return nil
}
