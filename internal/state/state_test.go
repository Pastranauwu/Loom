package state

import (
	"testing"
)

func TestGetDefaultPendiente(t *testing.T) {
	s, err := Get(t.TempDir(), "T-001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if s.Estado != Pendiente || s.ID != "T-001" {
		t.Errorf("Get sin archivo = %+v, quiero pendiente", s)
	}
}

func TestSaveGetRoundtrip(t *testing.T) {
	root := t.TempDir()
	s := State{
		ID:       "T-002",
		Estado:   EnCurso,
		Intentos: 2,
		Rama:     "loom/T-002",
		Puerto:   4321,
	}
	if err := Save(root, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Get(root, "T-002")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != s {
		t.Errorf("roundtrip = %+v, quiero %+v", got, s)
	}
}

func TestListYRemove(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"T-003", "T-001", "T-002"} {
		if err := Save(root, State{ID: id, Estado: Lista}); err != nil {
			t.Fatal(err)
		}
	}
	states, err := List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(states) != 3 {
		t.Fatalf("List devolvió %d, quiero 3", len(states))
	}
	for i, want := range []string{"T-001", "T-002", "T-003"} {
		if states[i].ID != want {
			t.Errorf("List[%d] = %s, quiero %s", i, states[i].ID, want)
		}
	}
	if err := Remove(root, "T-002"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := Remove(root, "T-002"); err != nil {
		t.Fatal("Remove dos veces no debió fallar")
	}
	states, _ = List(root)
	if len(states) != 2 {
		t.Errorf("List tras Remove = %d, quiero 2", len(states))
	}
}
