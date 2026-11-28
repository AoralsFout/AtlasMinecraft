package item

import (
	"atlas/internal/nbt"
	"testing"
)

func TestFlintAndSteelDamagePreservesSavedComponents(t *testing.T) {
	id, _ := ID(FlintAndSteel)
	fresh, err := New(id, 1).UseFlintAndSteel()
	if err != nil {
		t.Fatal(err)
	}
	if values, _, err := fresh.components(); err != nil || values["damage"] != int32(1) {
		t.Fatalf("first use: components %v, error %v", values, err)
	}
	tag, err := fresh.ToNBT()
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := tag.Object()
	components, _ := saved.Object("components")
	components["minecraft:custom_data"] = compound(nbt.Object{"kept": integer(42)})
	imported, err := FromNBT(tag)
	if err != nil {
		t.Fatal(err)
	}
	used, err := imported.UseFlintAndSteel()
	if err != nil {
		t.Fatal(err)
	}
	tag, err = used.ToNBT()
	if err != nil {
		t.Fatal(err)
	}
	saved, _ = tag.Object()
	components, _ = saved.Object("components")
	custom, _ := components.Object("minecraft:custom_data")
	damage, _ := components.Number("minecraft:damage")
	kept, _ := custom.Number("kept")
	roundTrip, err := FromNBT(tag)
	if err != nil || damage != 2 || kept != 42 || !roundTrip.SameItem(used) {
		t.Fatalf("saved damage %d, custom data %d, item round-trip error %v", damage, kept, err)
	}
}
