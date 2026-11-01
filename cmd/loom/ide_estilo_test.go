package main

import "testing"

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
