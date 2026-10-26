package main

import (
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/tui"
	"path/filepath"
)

// mostrarInicioProyecto orienta el arranque sin interrumpir ejecuciones automatizadas.
func mostrarInicioProyecto(root string) {
	if !esTUI() {
		out.Line(idioma.T("· preparando proyecto %s · repositorio → modelos → arquitectura → tareas"), filepath.Base(root))
		return
	}
	out.Line("%s", tui.Logo(72))
	out.Line("%s", tui.Caja(tui.Titulo("NUEVO PROYECTO · "+filepath.Base(root))+"\n\n"+
		"Describe el resultado en loom.specs.yml.\n"+
		"El planificador define arquitectura, archivos y contratos.\n"+
		"Los agentes implementan; las pruebas verifican la entrega.\n\n"+
		tui.Apagado(fmt.Sprintf("Carpeta  %s\nSiguiente  seleccionar ejecutor y preparar el entorno", root))))
}
