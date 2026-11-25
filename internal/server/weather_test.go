package server

import (
	"atlas/internal/hub"
	"atlas/internal/javasave"
	"atlas/internal/nbt"
	"atlas/internal/player"
	"atlas/internal/proto"
	"atlas/internal/world"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestWeatherPacketsAndSaveAgainstVanilla(t *testing.T) {
	file, err := os.Open("../../testdata/protocol/weather_1.21.1.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	seed := int64(1234)
	if err := world.CreateFlatWorld(dir, &seed); err != nil {
		t.Fatal(err)
	}
	open := func() (*Server, *player.Player, *player.Player) {
		h := hub.New()
		w, err := world.Open(h, 1, dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := w.Close(); err != nil {
				t.Error(err)
			}
		})
		if err := w.SetSpawnChunkRadius(0); err != nil {
			t.Fatal(err)
		}
		s := New(Config{}, h, w)
		clients := make([]*player.Player, 0, 2)
		for i, dim := range []string{"minecraft:overworld", "minecraft:the_nether"} {
			p := player.New(dim, [16]byte{byte(i + 1)}, int32(i+1), nil)
			p.SetInitialDimension(dim)
			p.SetPlayReady(true)
			if !h.Join(p) {
				t.Fatal("join", dim)
			}
			clients = append(clients, p)
		}
		return s, clients[0], clients[1]
	}
	s, overworld, nether := open()
	command := func(line, want string) {
		t.Helper()
		if got := s.executeCommand(nil, line); !strings.Contains(got, want) {
			t.Fatalf("%s: %s", line, got)
		}
	}
	packets := func(p *player.Player) string {
		var raw []string
		for len(p.Out) > 0 {
			pkt := <-p.Out
			if pkt.ID == proto.SGameStateChange {
				raw = append(raw, fmt.Sprintf("%x", pkt.Payload))
			}
		}
		return strings.Join(raw, " ")
	}
	command("gamerule doWeatherCycle false", "now set to: false")
	for i, row := range rows[1:] {
		switch row[0] {
		case "thunder 25", "clear 3":
			command("weather "+row[0], "Weather set to")
		case "join":
			s.sendWorldState(overworld)
		case "clear 7 cycle false":
			command("gamerule doWeatherCycle false", "now set to: false")
			command("weather clear 7t", "Weather set to clear")
		case "cycle true":
			command("gamerule doWeatherCycle true", "now set to: true")
		case "rain 5 cycle false":
			command("weather rain 5t", "Weather set to rain")
			command("gamerule doWeatherCycle false", "now set to: false")
		case "step":
			if err := s.TickWorlds(); err != nil {
				t.Fatal(err)
			}
		case "reload":
			if err := s.World.Close(); err != nil {
				t.Fatal(err)
			}
			s, overworld, nether = open()
			command("gamerule doWeatherCycle", "currently set to: false")
			s.sendWorldState(overworld)
			command("gamerule doWeatherCycle true", "now set to: true")
		default:
			t.Fatal("unknown reference action", row[0])
		}
		if got := packets(overworld); got != row[6] {
			t.Fatalf("row %d %s Overworld %s, want %s", i, row[0], got, row[6])
		}
		if got := packets(nether); got != row[7] {
			t.Fatalf("row %d %s Nether %s, want %s", i, row[0], got, row[7])
		}
		if err := s.World.Save(); err != nil {
			t.Fatal(err)
		}
		data, err := javasave.ReadGzip(filepath.Join(dir, "level.dat"))
		if err != nil {
			t.Fatal(err)
		}
		_, tag, err := nbt.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		root, _ := tag.Object()
		level, _ := root.Object("Data")
		for j, name := range rows[0][1:6] {
			v, ok := level.Number(name)
			if !ok || strconv.FormatInt(v, 10) != row[j+1] {
				t.Fatalf("row %d %s %s %d, want %s", i, row[0], name, v, row[j+1])
			}
		}
	}
}
