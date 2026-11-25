package server

import (
	"atlas/internal/world"
	"errors"
	"strings"
	"testing"
)

func TestWorldClockAdvancesAllDimensionsAndCommandsChangeOnlyDayTime(t *testing.T) {
	s, p, _ := dimensionTestServer(t)
	startAge, _ := s.World.Time()
	if err := s.TickWorlds(nil); err != nil {
		t.Fatal(err)
	}
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			if _, err := s.World.LoadChunk(x, z); err != nil {
				t.Fatal(err)
			}
		}
	}
	sand, err := world.BlockStateID("minecraft:sand", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.World.SetBlock(8, -48, 8, sand); err != nil {
		t.Fatal(err)
	}
	if got := s.executeCommand(p, "time set 1d"); !strings.Contains(got, "24000") {
		t.Fatal(got)
	}
	if got := s.executeCommand(p, "time add 0.5s"); !strings.Contains(got, "Added 10 ticks") {
		t.Fatal(got)
	}
	for _, w := range s.Worlds() {
		if age, day := w.Time(); age != startAge+1 || day != 24010 {
			t.Fatalf("%s after time commands: age=%d day=%d", w.DimensionName(), age, day)
		}
	}
	center := func(x, z int32) bool { return x == 0 && z == 0 }
	chunks := map[string]world.TickChunks{s.World.DimensionName(): {Blocks: center, Entities: center}}
	if err := s.TickWorlds(chunks); err != nil {
		t.Fatal(err)
	}
	for _, w := range s.Worlds() {
		if age, day := w.Time(); age != startAge+2 || day != 24011 {
			t.Fatalf("%s after second tick: age=%d day=%d", w.DimensionName(), age, day)
		}
	}
	if got, err := s.World.BlockAt(8, -48, 8); err != nil || got != sand {
		t.Fatalf("sand should still wait for its second game tick: block=%d, err=%v", got, err)
	}
	if err := s.TickWorlds(chunks); err != nil {
		t.Fatal(err)
	}
	if got, err := s.World.BlockAt(8, -48, 8); err != nil || got != world.StateAir {
		t.Fatalf("scheduled sand should become a falling entity: block=%d, err=%v", got, err)
	}
}

func TestWorldClockContinuesAfterDimensionFailure(t *testing.T) {
	s, _, _ := dimensionTestServer(t)
	age, day := s.World.Time()
	if err := s.World.Close(); err != nil {
		t.Fatal(err)
	}
	err := s.TickWorlds(nil)
	if !errors.Is(err, world.ErrWorldClosed) || !strings.Contains(err.Error(), "minecraft:overworld") {
		t.Fatalf("expected a dimension-specific error, got %v", err)
	}
	for _, w := range s.Worlds() {
		if w == s.World {
			continue
		}
		if gotAge, gotDay := w.Time(); gotAge != age+1 || gotDay != day+1 {
			t.Fatalf("%s did not tick after overworld failure: age=%d day=%d", w.DimensionName(), gotAge, gotDay)
		}
	}
}
