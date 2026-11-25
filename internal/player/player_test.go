package player

import "testing"

func TestSimulationPositionTracksModeAndDimension(t *testing.T) {
	p := New("observer", [16]byte{}, 1, nil)
	if _, _, _, _, active := p.SimulationPosition(); active {
		t.Fatal("login in progress contributed a player")
	}
	p.SetPlayReady(true)
	p.SetPos(-7.25, 310, 15.75, 0, 0, false)
	dim, x, z, spectator, active := p.SimulationPosition()
	if dim != "minecraft:overworld" || x != -7.25 || z != 15.75 || spectator || !active || p.GameMode() != CreativeMode {
		t.Fatal("creative snapshot", dim, x, z, spectator, active)
	}
	p.gameMode = SpectatorMode
	p.ResetWorld("minecraft:the_end", [3]float64{-.25, 64, -8.5}, 0, 0, nil)
	dim, x, z, spectator, active = p.SimulationPosition()
	if dim != "minecraft:the_end" || x != -.25 || z != -8.5 || !spectator || !active {
		t.Fatal("spectator snapshot", dim, x, z, spectator, active)
	}
	p.Disconnect()
	if _, _, _, _, active := p.SimulationPosition(); active {
		t.Fatal("disconnected observer contributed a player")
	}
}

func TestNegativeFractionalPositionUsesNegativeChunk(t *testing.T) {
	p := New("test", [16]byte{}, 1, nil)
	p.SetPos(-0.5, 70, -16.5, 0, 0, true)
	cx, cz := p.ChunkPos()
	if cx != -1 || cz != -2 {
		t.Fatalf("chunk = (%d,%d), want (-1,-2)", cx, cz)
	}
}
