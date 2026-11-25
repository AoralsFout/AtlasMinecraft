package world

import (
	"atlas/internal/worldgen"
	"testing"
)

func TestNaturalWeatherTimerRenewal(t *testing.T) {
	// Java 1.21.1 ServerLevel.THUNDER_DELAY / THUNDER_DURATION, followed
	// by RAIN_DELAY / RAIN_DURATION, sampled with LegacyRandomSource(1234).
	for _, tc := range []struct {
		thunder, rain         bool
		thunderTime, rainTime int32
	}{{false, false, 176364, 143313}, {false, true, 176364, 16146}, {true, false, 12528, 143313}, {true, true, 12528, 16146}} {
		w := New(noViewers{}, 1)
		w.tickRandom = worldgen.NewLegacyRandom(1234)
		w.meta.weatherTimers = weatherTimers{Raining: tc.rain, Thundering: tc.thunder}
		if err := w.Tick(TickChunks{}); err != nil {
			t.Fatal(err)
		}
		want := weatherTimers{ThunderTime: tc.thunderTime, RainTime: tc.rainTime, Thundering: tc.thunder, Raining: tc.rain}
		if got := w.meta.weatherTimers; got != want {
			t.Fatalf("timer renewal got %+v want %+v", got, want)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
