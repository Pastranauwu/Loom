package tui

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/topo"
)

// EnObra reporta si queda trabajo sin terminar en el mapa: mientras
// haya algo pendiente, en curso o detenido, el mapa sigue a las tareas;
// con todo listo, muestra la arquitectura.
func EnObra(m topo.Mapa) bool {
	c := m.Compuertas.Tareas
	return c["pendiente"]+c["en_curso"]+c["detenida"] > 0
}

// VistaArquitectura es el diagrama del proyecto ya entregado: una franja por
// capa, de las entradas a los cimientos, unidas por flechas. Cada módulo es
// una tarjeta que llena el ancho disponible con qué hace (su propósito), qué
// contiene (tipos y funciones principales) y de qué depende. Las tareas no
// aparecen: lo que se ve es lo que hay.
func VistaArquitectura(m topo.Mapa, ancho int) string {
	t := etiquetas()
	var b strings.Builder
	bloques, flechas := 0, 0
	for _, mod := range m.Modulos {
		if !topo.EsAndamio(mod.Ruta) {
			bloques++
		}
	}
	for _, a := range m.Aristas {
		if !topo.EsAndamio(a.De) && !topo.EsAndamio(a.A) {
			flechas++
		}
	}
	b.WriteString(estiloTitulo.Render(idioma.T("arquitectura")) + "  " +
		estiloApagado.Render(fmt.Sprintf("%d %s · %d %s", bloques, t.modulos, flechas, t.dependencias)) + "\n")
	b.WriteString(estiloApagado.Width(ancho).Render(idioma.T("arriba las entradas, abajo los cimientos · cada flecha baja a lo que usa · loom map <módulo> explica uno")) + "\n\n")

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
		if !topo.EsAndamio(a.A) {
			deps[a.De] = append(deps[a.De], nombre(a.A))
		}
	}
	// tarjetas de lado a lado solo si caben con al menos 38 columnas cada una;
	// cada fila reparte el ancho entero entre las suyas
	maxCols := 1
	if ancho >= 80 { // más angosto, dos columnas apilan el texto de cada archivo en tiras
		maxCols = max(1, (ancho+1)/39)
	}
	primera := true
	for capa := 0; capa < m.NumCapas; capa++ {
		var mods []topo.Modulo
		for _, mod := range m.Modulos {
			if mod.Capa == capa && !topo.EsAndamio(mod.Ruta) {
				mods = append(mods, mod)
			}
		}
		if len(mods) == 0 {
			continue
		}
		if !primera {
			b.WriteString(estiloApagado.Render("  │\n  ▼") + "\n")
		}
		primera = false
		b.WriteString(bandaCapa(capa, m.NumCapas, ancho, t) + "\n")
		for i := 0; i < len(mods); i += maxCols {
			fila := mods[i:min(i+maxCols, len(mods))]
			w := max((ancho-(len(fila)-1))/len(fila), 20)
			var tarjetas []string
			for _, mod := range fila {
				d := unicoOrden(deps[mod.Ruta])
				sort.Strings(d)
				tarjetas = append(tarjetas, bloque(mod, nombre(mod.Ruta), d, w, t))
			}
			alto := 0
			for _, c := range tarjetas {
				alto = max(alto, lipgloss.Height(c))
			}
			for k, mod := range fila {
				if lipgloss.Height(tarjetas[k]) < alto { // misma altura: la fila se lee como cuadrícula
					d := unicoOrden(deps[mod.Ruta])
					sort.Strings(d)
					tarjetas[k] = bloqueAlto(mod, nombre(mod.Ruta), d, w, alto, t)
				}
			}
			b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, intercalar(tarjetas)...) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// bloque es un módulo en una tarjeta: nombre y tamaño, qué hace, qué
// contiene y de qué depende, con el texto ajustado al ancho en vez de
// cortado. El color del borde y del nombre dice el estado de sus tareas.
func bloque(mod topo.Modulo, nombre string, deps []string, ancho int, t etiquetasMapa) string {
	return bloqueAlto(mod, nombre, deps, ancho, 0, t)
}

// bloqueAlto es bloque con altura total fija (0: la que pida el contenido).
func bloqueAlto(mod topo.Modulo, nombre string, deps []string, ancho, alto int, t etiquetasMapa) string {
	in := ancho - 4 // borde y relleno
	color := colorModulo(mod)
	if color == apagado {
		color = borde
	}
	nom := lipgloss.NewStyle().Foreground(colorModulo(mod)).Bold(true)
	if colorModulo(mod) == apagado {
		nom = estiloTitulo // sin tareas: el acento del tema, como el resto de la marca
	}
	sub := cuenta(mod.Archivos, t.archivo, t.archivos)
	if n := len(mod.Expone); n > 0 {
		sub += fmt.Sprintf(" · %s%d", ic().Contrato, n)
	}
	ajusta := lipgloss.NewStyle().Width(in)
	nombre = conIcono(iconoDe(mod.Lenguaje), nombre)
	l := []string{nom.Render(recorte(nombre, in)) + estiloApagado.Render(" · "+recorte(sub, max(in-lipgloss.Width(nombre)-3, 1)))}
	if mod.Proposito != "" {
		l = append(l, ajusta.Render(mod.Proposito))
	}
	for i, p := range mod.Contenido {
		if i == 4 {
			l = append(l, estiloApagado.Render(fmt.Sprintf("+%d %s", len(mod.Contenido)-4, t.archivos)))
			break
		}
		l = append(l, lineaArchivo(conIcono(iconoDe(p.Archivo), strings.TrimSuffix(p.Archivo, path.Ext(p.Archivo))), p.Doc, in))
	}
	if c := contenido(mod); c != "" {
		l = append(l, estiloApagado.Render(ajusta.Render(ic().Simbolo+" "+c)))
	}
	if len(deps) > 0 {
		l = append(l, estiloApagado.Render(ajusta.Render("→ "+strings.Join(deps, ", "))))
	}
	caja := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(color).Padding(0, 1).Width(ancho - 2)
	if alto > 2 {
		caja = caja.Height(alto - 2)
	}
	return caja.Render(strings.Join(l, "\n"))
}

// lineaArchivo es "nombre línea del encabezado" ajustada al ancho: primero
// se parte el texto plano y después se pinta cada renglón, porque un estilo
// pintado antes de partir se pierde en el salto y los renglones siguientes
// salían con otro color.
func lineaArchivo(arch, doc string, in int) string {
	ls := strings.Split(lipgloss.NewStyle().Width(in).Render(arch+" "+doc), "\n")
	for i, l := range ls {
		l = strings.TrimRight(l, " ")
		if i == 0 && strings.HasPrefix(l, arch) {
			ls[i] = estiloTitulo.Render(arch) + estiloApagado.Render(strings.TrimPrefix(l, arch))
		} else {
			ls[i] = estiloApagado.Render(l)
		}
	}
	return strings.Join(ls, "\n")
}

// contenido nombra lo principal que define el módulo (tipos y funciones,
// sin métodos), para saber qué hay dentro sin abrirlo.
func contenido(mod topo.Modulo) string {
	var n []string
	for _, s := range mod.Simbolos {
		if x := task.NombreDeFirma(s); x != "" && len(n) < 4 {
			n = append(n, x)
		}
	}
	if len(n) == 0 {
		return ""
	}
	if resto := mod.NumSimbolos - len(n); resto > 0 && len(n) == 4 {
		return strings.Join(n, " · ") + fmt.Sprintf(" +%d", resto)
	}
	return strings.Join(n, " · ")
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

func baseModulo(ruta string) string {
	if ruta == "." || ruta == "" {
		return idioma.T("raíz")
	}
	return ruta[strings.LastIndex(ruta, "/")+1:]
}
