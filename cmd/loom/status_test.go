package main

import "testing"

func TestEstadoRepoEnUnaLinea(t *testing.T) {
	e := estadoRepo{Rama: "loom/_entrega", Base: "main", BaseTip: "abc1234", IntegraTip: "def5678", IntegraMas: 4, SinEntregar: []string{"T-003", "T-005"}, Detenidas: []string{"T-008"}}
	want := "base main@abc1234 · estás en loom/_entrega · _integra def5678 (+4) · sin entregar: T-003, T-005 · detenidas: T-008"
	if got := e.String(); got != want {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
}
