package main

import "testing"

func TestParseVisual(t *testing.T) {
	cumple, cambios, ok := parseVisual("Miré las capturas.\n```json\n{\"cumple\": false, \"cambios\": [\"el fondo es plano\"]}\n```")
	if !ok || cumple || len(cambios) != 1 {
		t.Fatalf("%v %v %v", cumple, cambios, ok)
	}
	if _, _, ok := parseVisual("no pude abrir las imágenes"); ok {
		t.Error("sin JSON no hay veredicto")
	}
}
