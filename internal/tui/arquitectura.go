package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/topo"
)

// EnObra reporta si queda trabajo sin terminar en el mapa: mientras
// haya algo pendiente, en curso o detenido, el mapa sigue a las tareas;
// con todo listo, muestra la arquitectura.
func EnObra(m topo.Mapa) bool {
	c := m.Compuertas.Tareas
	return c["pendiente"]+c["en_curso"]+c["detenida"] > 0
}

// VistaArquitectura es el bosquejo del proyecto ya entregado: cada capa es
// una franja de bloques chicos (módulo, archivos, contratos que expone),
// acomodados de lado a lado, y debajo de cada franja de qué bloques
// dependen. Las tareas no aparecen: lo que se ve es lo que hay.
func VistaArquitectura(m topo.Mapa, ancho int) string {
	t := etiquetas()
	var b strings.Builder
	bloques, flechas := 0, 0
	for _, mod := range m.Modulos {
		if !esAndamio(mod.Ruta) {
			bloques++
		}
	}
	for _, a := range m.Aristas {
		if !esAndamio(a.De) && !esAndamio(a.A) {
			flechas++
		}
	}
	b.WriteString(estiloTitulo.Render(idioma.T("arquitectura")) + "  " +
		estiloApagado.Render(fmt.Sprintf("%d %s · %d %s", bloques, t.modulos, flechas, t.dependencias)) + "\n\n")

	// el nombre corto basta salvo que dos módulos lo compartan
	veces := map[string]int{}
	for _, mod := range m.Modulos {
		veces[baseModulo(mod.Ruta)]++
	}
	nombre := func(ruta string) string {
		if veces[baseModulo(ruta)] > 1 {
			return ruta
		}
		return baseModulo(ruta)
	}
	deps := map[string][]string{}
	for _, a := range m.Aristas {
		if !esAndamio(a.A) {
			deps[a.De] = append(deps[a.De], nombre(a.A))
		}
	}
	for capa := 0; capa < m.NumCapas; capa++ {
		var mods []topo.Modulo
		for _, mod := range m.Modulos {
			if mod.Capa == capa && !esAndamio(mod.Ruta) {
				mods = append(mods, mod)
			}
		}
		if len(mods) == 0 {
			continue
		}
		b.WriteString(bandaCapa(capa, m.NumCapas, ancho, t) + "\n")
		var bloques []string
		for _, mod := range mods {
			bloques = append(bloques, bloque(mod, nombre(mod.Ruta), ancho))
		}
		b.WriteString(enFilas(bloques, ancho) + "\n")
		// de qué depende cada bloque, como `tree`: una rama por bloque
		var ramas []topo.Modulo
		for _, mod := range mods {
			if len(deps[mod.Ruta]) > 0 {
				ramas = append(ramas, mod)
			}
		}
		for i, mod := range ramas {
			d := unicoOrden(deps[mod.Ruta])
			sort.Strings(d)
			rama := "├─ "
			if i == len(ramas)-1 {
				rama = "└─ "
			}
			n := nombre(mod.Ruta)
			b.WriteString(estiloApagado.Render(rama) + estiloBold.Render(n) + estiloApagado.Render(" → "+lista(d, ancho-8-lipgloss.Width(n))) + "\n")
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// bloque es un módulo en una caja fina de una línea: nombre y, apagado,
// cuántos archivos tiene y cuántos contratos expone. El color del borde
// y del nombre dice el estado de sus tareas.
func bloque(mod topo.Modulo, nombre string, ancho int) string {
	sub := fmt.Sprintf(" %d", mod.Archivos)
	if n := len(mod.Expone); n > 0 {
		sub += fmt.Sprintf(" ◆%d", n)
	}
	color := colorModulo(mod)
	if color == apagado {
		color = borde
	}
	nom := lipgloss.NewStyle().Foreground(colorModulo(mod)).Bold(true)
	if colorModulo(mod) == apagado {
		nom = estiloTitulo // sin tareas: el acento del tema, como el resto de la marca
	}
	return lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(color).Padding(0, 1).
		Render(nom.Render(recorte(nombre, max(ancho-12, 8))) + estiloApagado.Render(sub))
}

// lista junta nombres hasta n columnas y cuenta el resto como +N.
func lista(nombres []string, n int) string {
	out := ""
	for i, x := range nombres {
		sig := x
		if i > 0 {
			sig = out + ", " + x
		}
		resto := fmt.Sprintf(" +%d", len(nombres)-i)
		if lipgloss.Width(sig)+len(resto) > n && i < len(nombres)-1 || lipgloss.Width(sig) > n {
			return out + resto
		}
		out = sig
	}
	return out
}

// enFilas pone los bloques de lado a lado y salta de fila al llenar ancho.
func enFilas(bloques []string, ancho int) string {
	var filas, fila []string
	usado := 0
	for _, bl := range bloques {
		w := lipgloss.Width(bl)
		if len(fila) > 0 && usado+1+w > ancho {
			filas = append(filas, lipgloss.JoinHorizontal(lipgloss.Top, fila...))
			fila, usado = nil, 0
		}
		if len(fila) > 0 {
			fila = append(fila, " ")
			usado++
		}
		fila = append(fila, bl)
		usado += w
	}
	if len(fila) > 0 {
		filas = append(filas, lipgloss.JoinHorizontal(lipgloss.Top, fila...))
	}
	return strings.Join(filas, "\n")
}

func baseModulo(ruta string) string {
	if ruta == "." || ruta == "" {
		return idioma.T("raíz")
	}
	return ruta[strings.LastIndex(ruta, "/")+1:]
}

// esAndamio reconoce pruebas, migraciones y fixtures: sostienen el código
// pero no son bloques de la arquitectura, y llenaban los puntos de entrada.
func esAndamio(ruta string) bool {
	switch baseModulo(ruta) {
	case "tests", "test", "__tests__", "spec", "specs", "e2e", "migrations", "fixtures", "testdata":
		return true
	}
	return false
}
