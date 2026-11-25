package server

import (
	"atlas/internal/item"
	"atlas/internal/proto"
	"atlas/internal/world"
	"context"
	"testing"
)

func serverTestPortal(t *testing.T, w *world.World, base world.BlockPosition, axis string, fill bool) {
	t.Helper()
	obsidian, _ := world.BlockStateID("minecraft:obsidian", nil)
	portal, _ := world.BlockStateID("minecraft:nether_portal", map[string]string{"axis": axis})
	for x := int32(-1); x <= 2; x++ {
		for y := int32(-1); y <= 3; y++ {
			p := base
			p.Y += y
			if axis == "x" {
				p.X += x
			} else {
				p.Z += x
			}
			state := obsidian
			if x >= 0 && x < 2 && y >= 0 && y < 3 {
				state = world.StateAir
				if fill {
					state = portal
				}
			}
			if err := w.SetBlock(p.X, p.Y, p.Z, state); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// The 1.21.1 dedicated-server trace used an End portal at (104,49,0)
// and a forced personal spawn in an otherwise empty Overworld column.
func TestEndPortalRoundTripCreditsAndTicketLifetime(t *testing.T) {
	s, p, _ := dimensionTestServer(t)
	end, _ := s.WorldInDimension("minecraft:the_end")
	nether, _ := s.WorldInDimension("minecraft:the_nether")
	portal, _ := world.BlockStateID("minecraft:end_portal", nil)
	sand, _ := world.BlockStateID("minecraft:sand", nil)
	check := func() {
		t.Helper()
		if !p.BeginPortalCheck() {
			t.Fatal("portal check unexpectedly blocked")
		}
		s.checkPlayerPortal(p, 1)
	}
	set := func(w *world.World, x, y, z int32, id uint32) {
		t.Helper()
		if err := w.SetBlock(x, y, z, id); err != nil {
			t.Fatal(err)
		}
	}
	tick := func(w *world.World, n int) {
		t.Helper()
		for range n {
			if err := w.TickPlayers(nil, 1); err != nil {
				t.Fatal(err)
			}
		}
	}
	assertBlock := func(w *world.World, x, y, z int32, want uint32) {
		t.Helper()
		got, err := w.BlockAt(x, y, z)
		if err != nil || got != want {
			t.Fatalf("block %d,%d,%d=%d want %d (%v)", x, y, z, got, want, err)
		}
	}
	if err := s.World.SetSpawnChunkRadius(0); err != nil {
		t.Fatal(err)
	}
	if err := s.World.SetPlayerSpawn(p.UUID, -1592, 70, -1592, 37); err != nil {
		t.Fatal(err)
	}
	set(end, 104, 49, 0, portal)
	for i, origin := range []*world.World{s.World, nether} {
		// Repeat from the Nether to exercise both entry dimensions.
		p.SetInitialDimension(origin.DimensionName())
		set(origin, 1608, 79, 1608, portal)
		p.SetPos(1608.5, 79.5, 1608.5, 12, 21, false)
		check()
		x, y, z, yaw, pitch, _ := p.Pos()
		if p.Dimension() != end.DimensionName() || x != 100.5 || y != 49 || z != .5 || yaw != 90 || pitch != 21 {
			t.Fatalf("entry %d: %s %v", i, p.Dimension(), []any{x, y, z, yaw, pitch})
		}
		p.ConfirmTeleport(s.nextTeleportID.Load())
		for range 10 {
			check()
		}
		p.SetPos(104.5, 49.5, .5, 0, 0, false)
		check()
		winEvents := 0
		for len(p.Out) > 0 {
			packet := <-p.Out
			if packet.ID == proto.SGameStateChange && len(packet.Payload) == 5 && packet.Payload[0] == 4 {
				winEvents++
			}
		}
		if i == 0 {
			if winEvents != 1 || !p.AwaitingEndCredits() || p.BeginPortalCheck() {
				t.Fatal("first return did not suspend play for credits")
			}
			if _, _, _, _, active := p.SimulationPosition(); active {
				t.Fatal("credits player still drives simulation")
			}
			s.handlePlayPacket(p, proto.CClientCommand, []byte{1})
			if !p.AwaitingEndCredits() {
				t.Fatal("stats request dismissed credits")
			}
			s.handlePlayPacket(p, proto.CClientCommand, []byte{0})
			var flags byte
			for len(p.Out) > 0 {
				packet := <-p.Out
				if packet.ID == proto.SRespawn {
					flags = packet.Payload[len(packet.Payload)-1]
				}
			}
			if flags != 1 {
				t.Fatalf("credits respawn keep flags = %d", flags)
			}
		} else if winEvents != 0 {
			t.Fatal("repeat exit replayed credits")
		}
		x, y, z, yaw, pitch, _ = p.Pos()
		if p.Dimension() != s.World.DimensionName() || x != -1591.5 || y != 70.1 || z != -1591.5 || yaw != 37 || pitch != 0 || !p.PlayReady() {
			t.Fatalf("return %d: %s %v", i, p.Dimension(), []any{x, y, z, yaw, pitch})
		}
		id := s.nextTeleportID.Load()
		s.handlePlayPacket(p, proto.CClientCommand, []byte{0})
		if id != s.nextTeleportID.Load() {
			t.Fatal("duplicate credits acknowledgement teleported twice")
		}
		p.ConfirmTeleport(id)
		for range 10 {
			check()
		}
		if i == 0 {
			tick(end, 115)
		}
	}
	s.savePlayer(p)
	saved, ok, err := s.World.LoadPlayer(p.UUID)
	if err != nil || !ok || !saved.SeenCredits {
		t.Fatal("missing saved seenCredits", saved, err)
	}
	set(s.World, -1592, 220, -1592, sand)
	tick(s.World, 2)
	assertBlock(s.World, -1592, 220, -1592, sand)
	// Native /perf samples were taken after terrain loading settled. Manual
	// tick stepping here can otherwise outrun the asynchronous loader.
	if err := end.PrepareDestination(context.Background(), 136>>4, 0); err != nil {
		t.Fatal(err)
	}
	tick(end, 298)
	set(end, 136, 220, 8, sand)
	tick(end, 2)
	assertBlock(end, 136, 220, 8, world.StateAir)
	tick(end, 1)
	set(end, 137, 220, 8, sand)
	tick(end, 2)
	assertBlock(end, 137, 220, 8, sand)
}

func TestPortalIgnitesThroughCreativeItemInteraction(t *testing.T) {
	for _, name := range []string{"minecraft:flint_and_steel", "minecraft:fire_charge"} {
		t.Run(name, func(t *testing.T) {
			s, p := interactionServer(t)
			base := world.BlockPosition{X: 0, Y: -60, Z: 0}
			serverTestPortal(t, s.World, base, "x", false)
			p.SetPos(.5, -60, 1.5, 0, 0, false)
			id, _ := item.ID(name)
			inventory, _ := p.Inventory()
			inventory.Slots[36] = item.New(id, 1)
			s.setInventory(p, inventory)
			s.handlePlace(p, placePayload(0, -61, 0, 1, 700, .5))
			assertAck(t, p, 700)
			state, err := s.World.BlockAt(0, -60, 0)
			if err != nil {
				t.Fatal(err)
			}
			block, err := world.BlockStateByID(state)
			if err != nil || block.Name != "minecraft:nether_portal" || block.Properties["axis"] != "x" {
				t.Fatal("item did not ignite valid frame", block, err)
			}
		})
	}
}

func TestPlayerPortalTravelRotationCooldownAndSave(t *testing.T) {
	s, p, _ := dimensionTestServer(t)
	nether, _ := s.WorldInDimension("minecraft:the_nether")
	serverTestPortal(t, s.World, world.BlockPosition{X: 0, Y: -60, Z: 0}, "x", true)
	serverTestPortal(t, nether, world.BlockPosition{X: 0, Y: 80, Z: 0}, "z", true)
	p.SetPos(.8, -60, .5, 30, 15, false)
	for tick := 0; tick < 2; tick++ {
		if !p.BeginPortalCheck() {
			t.Fatal("portal check overlap")
		}
		s.checkPlayerPortal(p, 1)
		if tick == 0 && p.Dimension() != "minecraft:overworld" {
			t.Fatal("creative portal activated one tick early")
		}
	}
	if p.Dimension() != "minecraft:the_nether" {
		t.Fatal("portal contact did not transfer")
	}
	_, _, _, yaw, pitch, _ := p.Pos()
	if yaw != 120 || pitch != 15 || p.PortalCooldown() != 10 {
		t.Fatal("arrival rotation/cooldown incorrect", yaw, pitch, p.PortalCooldown())
	}
	p.ConfirmTeleport(s.nextTeleportID.Load())
	for i := 0; i < 3; i++ {
		p.BeginPortalCheck()
		s.checkPlayerPortal(p, 1)
	}
	if p.Dimension() != "minecraft:the_nether" || p.PortalCooldown() != 9 {
		t.Fatal("standing in destination portal caused return or wrong cooldown", p.PortalCooldown())
	}
	s.savePlayer(p)
	saved, exists, err := s.World.LoadPlayer(p.UUID)
	if err != nil || !exists || saved.Dimension != "minecraft:the_nether" || saved.PortalCooldown != 9 {
		t.Fatal("portal save lost dimension/cooldown", saved, err)
	}
	// With the traveler gone, only the successful crossing keeps scheduled
	// ticks running in the destination. No player tickets have been issued.
	s.Hub.Leave(p)
	sand, err := world.BlockStateID("minecraft:sand", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := nether.SetBlock(8, 224, 8, sand); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := nether.TickPlayers(nil, 1); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := nether.BlockAt(8, 224, 8); err != nil || got != world.StateAir {
		t.Fatalf("portal destination sand did not fall after departure: %d %v", got, err)
	}
}

func TestPlayerEndPortalArrivalUsesOriginalPlatform(t *testing.T) {
	s, p, _ := dimensionTestServer(t)
	state, _ := world.BlockStateID("minecraft:end_portal", nil)
	if err := s.World.SetBlock(0, -60, 0, state); err != nil {
		t.Fatal(err)
	}
	p.SetPos(.5, -59.5, .5, 15, 20, false)
	p.BeginPortalCheck()
	s.checkPlayerPortal(p, 1)
	x, y, z, yaw, pitch, _ := p.Pos()
	if p.Dimension() != "minecraft:the_end" || x != 100.5 || y != 49 || z != .5 || yaw != 90 || pitch != 20 {
		t.Fatal("End portal arrival incorrect", p.Dimension(), x, y, z, yaw, pitch)
	}
	end, _ := s.WorldInDimension("minecraft:the_end")
	block, err := end.BlockAt(100, 48, 0)
	obsidian, _ := world.BlockStateID("minecraft:obsidian", nil)
	if err != nil || block != obsidian {
		t.Fatal("End portal omitted arrival platform", err)
	}
}
