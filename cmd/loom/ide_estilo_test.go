package main

import (
	"os/exec"
	"strings"
	"testing"
)

// Las cajas cubren la ventana entre cabecera y base sin encimarse.
func TestCajasIDE(t *testing.T) {
	for _, d := range [][2]int{{120, 36}, {80, 24}, {237, 60}} {
		w, h := d[0], d[1]
		c, n, u := cajasIDE(w, h)
		if c[0] != 0 || c[1] != 1 || c[3] != h-2 {
			t.Errorf("%dx%d claude %v", w, h, c)
		}
		if n[0] != c[2] || n[2] != w-c[2] || u[0] != n[0] || u[2] != n[2] {
			t.Errorf("%dx%d columnas %v %v %v", w, h, c, n, u)
		}
		if n[1] != 1 || u[1] != n[1]+n[3] || u[1]+u[3] != h-1 || u[3] != usoCaja {
			t.Errorf("%dx%d filas %v %v", w, h, n, u)
		}
	}
}

func TestEstiloIDEMouse(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("falta tmux")
	}
	s := "test-estilo-mouse"
	if err := exec.Command("tmux", "new-session", "-d", "-s", s, "sleep 10").Run(); err != nil {
		t.Skipf("no se pudo crear sesión tmux: %v", err)
	}
	defer func() { _ = exec.Command("tmux", "kill-session", "-t", s).Run() }()

	estiloIDE(s, t.TempDir(), "loom")

	o, err := exec.Command("tmux", "list-keys", "-T", "root").Output()
	if err != nil {
		t.Fatalf("list-keys: %v", err)
	}
	out := string(o)
	if !strings.Contains(out, "WheelUpPane") || !strings.Contains(out, "WheelDownPane") {
		t.Errorf("falta WheelUpPane o WheelDownPane en root:\n%s", out)
	}
	if !strings.Contains(out, "panel nav") {
		t.Errorf("WheelUpPane no contiene lógica para panel nav:\n%s", out)
	}
}
