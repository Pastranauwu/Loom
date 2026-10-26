package main

import "testing"

func TestExigeDocker(t *testing.T) {
	casos := []struct {
		pedido string
		vacio  bool
		quiere bool
	}{
		{"api de tareas", true, true},
		{"api de tareas, sin docker", true, false},
		{"clasificador sobre el modelo existente", false, false},
		{"clasificador, con docker", false, true},
		{"con docker pero sin docker", false, false},
	}
	for _, c := range casos {
		if got := exigeDocker(c.pedido, c.vacio); got != c.quiere {
			t.Errorf("exigeDocker(%q, vacio=%v) = %v", c.pedido, c.vacio, got)
		}
	}
}
