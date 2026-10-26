package tui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbletea"

	"github.com/Pastranauwu/loom/internal/budget"
	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/recurse"
	"github.com/Pastranauwu/loom/internal/standup"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/ventanas"
)

// Fila es una tarea del tablero, con sus subtareas si viene de una tarea
// recursiva — Hijos queda vacío en el caso normal.
type Fila struct {
	ID     string
	Titulo string
	Estado string
	Hijos  []Fila

	// Detalle es lo que la tarea está haciendo ahora mismo (intento,
	// fase, modelo, tiempo en fase). Vacío si no está corriendo.
	Detalle string
	// Atascada marca que la fase actual lleva más de standup.UmbralAtasco
	// sin moverse. No la mata: solo avisa.
	Atascada bool
}

// Tablero lee las tareas y sus estados del disco, con el árbol de
// subtareas de cada una que haya recursado (internal/recurse).
func Tablero(root string) ([]Fila, error) {
	tasks, err := task.List(config.TasksDir(root))
	if err != nil {
		return nil, err
	}
	filas := make([]Fila, 0, len(tasks))
	for _, t := range tasks {
		s, err := state.Get(root, t.ID)
		if err != nil {
			return nil, err
		}
		nodos, err := recurse.LeerArbol(root, t.ID)
		if err != nil {
			return nil, err
		}
		f := Fila{ID: t.ID, Titulo: t.Titulo, Estado: s.Estado, Hijos: hijosDe(root, nodos, t.ID)}
		// el latido es lo único que sabe qué pasa DENTRO de un intento:
		// attempts.jsonl no se escribe hasta que el intento termina
		if l, corriendo := loop.LeerLatido(root, t.ID); corriendo {
			f.Detalle = l.Descripcion() + " · " + reloj(l.EnFaseDesde())
			if l.EnFaseDesde() >= standup.UmbralAtasco {
				f.Atascada = true
				f.Detalle = "ATASCO · " + f.Detalle + " sin señal"
			}
		} else if l, muerta := loop.Interrumpida(root, t.ID); muerta {
			// la corrida murió encima de la tarea (SIGKILL). Se marca
			// como atascada para que el tablero la pinte en rojo: es lo
			// que el humano tiene que ver al volver.
			f.Atascada = true
			f.Detalle = "INTERRUMPIDA · sin señal hace " + reloj(l.Silencio()) + " · run --reintentar"
		}
		filas = append(filas, f)
	}
	// el orden es el de la pantalla (por columna y dentro por id): el
	// cursor recorre filas, y ordenadas solo por id saltaba de una columna
	// a otra en vez de bajar a la línea de abajo
	sort.SliceStable(filas, func(i, j int) bool {
		if a, b := rangoEstado(filas[i].Estado), rangoEstado(filas[j].Estado); a != b {
			return a < b
		}
		return filas[i].ID < filas[j].ID
	})
	return filas, nil
}

// columnas es el orden en que el tablero agrupa las tareas.
var columnas = []struct {
	nombre string
	estado string
	glifo  string
	color  [3]int
}{
	{"LISTO PARA ENTREGAR", state.Lista, "✓", rgbPresion},
	{"EN CURSO", state.EnCurso, "◐", rgbEspera},
	{"DETENIDO", state.Detenida, "⏸", rgbAlerta},
	{"PENDIENTE", state.Pendiente, "·", rgbApagado},
}

func rangoEstado(estado string) int {
	for i, c := range columnas {
		if c.estado == estado {
			return i
		}
	}
	return len(columnas)
}

// hijosDe arma recursivamente el árbol de un padre a partir de los nodos
// planos guardados en arbol.json. Una subtarea con latido vivo — que
// corre ahora mismo — se pinta en curso con su detalle (intento, fase,
// modelo, tiempo), igual que la tarea raíz.
func hijosDe(root string, nodos []recurse.NodoArbol, padre string) []Fila {
	var hijos []Fila
	for _, n := range nodos {
		if n.Padre != padre {
			continue
		}
		estado := state.Detenida
		if n.Verde {
			estado = state.Lista
		}
		h := Fila{ID: n.ID, Titulo: n.Titulo, Estado: estado, Hijos: hijosDe(root, nodos, n.ID)}
		if l, corriendo := loop.LeerLatido(root, n.ID); corriendo {
			h.Estado = state.EnCurso
			h.Detalle = l.Descripcion() + " · " + reloj(l.EnFaseDesde())
			if l.EnFaseDesde() >= standup.UmbralAtasco {
				h.Atascada = true
				h.Detalle = "ATASCO · " + h.Detalle + " sin señal"
			}
		}
		hijos = append(hijos, h)
	}
	sort.Slice(hijos, func(i, j int) bool { return hijos[i].ID < hijos[j].ID })
	return hijos
}

func cursorInicial(filas []Fila) int {
	for i, f := range filas {
		if f.Estado == state.Lista {
			return i
		}
	}
	return 0
}

func selectedID(filas []Fila, cursor int) string {
	if cursor < 0 || cursor >= len(filas) {
		return ""
	}
	return filas[cursor].ID
}

// lineasPresupuesto arma las líneas de gasto si la corrida tiene tope
// (`presupuesto_tokens` o el bloque `presupuesto:` en config.yml), o nil
// para no pintar nada. La primera es el tope absoluto; las siguientes,
// una por proveedor con sus ventanas rodantes.
func lineasPresupuesto(root string) []string {
	cfg, err := config.Load(root)
	if err != nil {
		return nil
	}
	var lineas []string
	if cfg.PresupuestoTokens > 0 {
		lineas = append(lineas, budget.Barra(budget.GastoEnDisco(root), cfg.PresupuestoTokens))
	}
	registro := ventanas.Nuevo(ventanas.LedgerPath(), cfg.PresupuestoVentanas)
	for _, p := range config.Clis {
		if l := ventanas.LineaVentanas(registro, p); l != "" {
			lineas = append(lineas, l)
		}
	}
	return lineas
}

// filaConHijos arma la línea de una fila y, debajo, su árbol de
// subtareas indentado — recursivo, así que una subtarea que a su
// vez recursó también se ve anidada.
func filaConHijos(f Fila, profundidad int, sel string) []lineaSticker {
	marca, color := "  ", rgbTinta
	if f.ID == sel {
		marca, color = "> ", rgbPresion
	}
	sangria := ""
	for i := 0; i < profundidad; i++ {
		sangria += "  "
	}
	glifo := ""
	if profundidad > 0 {
		glifo = "└ "
		if f.Estado == state.Detenida {
			color = rgbAlerta
		} else {
			color = rgbApagado
		}
	}
	texto := marca + sangria + glifo + f.ID + "  " + f.Titulo
	ls := []lineaSticker{{texto: texto, color: color}}
	if f.Detalle != "" {
		c := rgbApagado
		if f.Atascada {
			c = rgbAlerta
		}
		ls = append(ls, lineaSticker{texto: "    " + sangria + f.Detalle, color: c})
	}
	for _, h := range f.Hijos {
		ls = append(ls, filaConHijos(h, profundidad+1, sel)...)
	}
	return ls
}

// armarTablero es el tablero entero, sin recortar: lo que se ve cuando
// cabe en la terminal.
func armarTablero(filas []Fila, sel, aviso string, presupuesto []string) []lineaSticker {
	return encajarTablero(filas, sel, aviso, "", presupuesto, 0, 0)
}

// encajarTablero arma el tablero para una terminal de ancho × alto (0 =
// sin límite). Si no cabe, el logo se reduce a una línea y las tareas se
// ven por una ventana que sigue al cursor: antes lo que no entraba se
// cortaba sin aviso y con muchas tareas solo se veía el principio de la
// primera columna, sin forma de llegar al resto.
// filtro es la línea del filtro activo ("" si no hay): con él, una lista
// vacía es "nada coincide", no "sin tareas".
func encajarTablero(filas []Fila, sel, aviso, filtro string, presupuesto []string, ancho, alto int) []lineaSticker {
	var cab []lineaSticker
	n := len(logoFilas)
	for i, fila := range logoFilas {
		c := mezclarRGB([3]int{79, 179, 162}, [3]int{44, 110, 99}, float64(i)/float64(n-1))
		cab = append(cab, lineaSticker{texto: fila, color: c})
	}
	cab = append(cab, lineaSticker{}, lineaSticker{texto: "tú diseñas el patrón · loom teje el código", color: rgbApagado})
	var gasto []lineaSticker
	for _, l := range presupuesto {
		gasto = append(gasto, lineaSticker{texto: "presupuesto " + l, color: rgbApagado})
	}
	cab = append(append(cab, gasto...), lineaSticker{})

	if len(filas) == 0 && filtro == "" {
		return recortarAncho(append(cab, lineaSticker{texto: "sin tareas · empieza con loom plan \"lo que necesitas\"", color: rgbApagado}), ancho)
	}

	var cuerpo []lineaSticker
	lineaSel := 0
	for _, col := range columnas {
		var suyos []Fila
		for _, f := range filas {
			if f.Estado == col.estado {
				suyos = append(suyos, f)
			}
		}
		titulo := col.glifo + " " + col.nombre
		if len(suyos) > 0 {
			titulo += " · " + strconv.Itoa(len(suyos))
		}
		cuerpo = append(cuerpo, lineaSticker{texto: titulo, color: col.color})
		if len(suyos) == 0 {
			cuerpo = append(cuerpo, lineaSticker{texto: "  —", color: rgbApagado})
		}
		for _, f := range suyos {
			if f.ID == sel {
				lineaSel = len(cuerpo)
			}
			cuerpo = append(cuerpo, filaConHijos(f, 0, sel)...)
		}
		cuerpo = append(cuerpo, lineaSticker{})
	}

	var pie []lineaSticker
	if aviso != "" {
		pie = append(pie, lineaSticker{texto: aviso, color: rgbAlerta})
	}
	if filtro != "" {
		pie = append(pie, lineaSticker{texto: filtro, color: rgbEspera})
	}
	pie = append(pie, lineaSticker{texto: ayudaTablero, color: rgbApagado})

	libre := alto - 2*margenTablero
	if alto > 0 && len(cab)+len(cuerpo)+len(pie) > libre {
		cab = append([]lineaSticker{{texto: "loom · " + strconv.Itoa(len(filas)) + " tareas", color: rgbPresion}}, gasto...)
		// dos líneas fijas para "más arriba / más abajo": si aparecieran
		// y desaparecieran, la lista saltaría al mover el cursor
		visibles := libre - len(cab) - len(pie) - 2
		if visibles < 1 {
			visibles = 1
		}
		desde, hasta := ventana(lineaSel, len(cuerpo), visibles)
		arriba, abajo := lineaSticker{}, lineaSticker{}
		if desde > 0 {
			arriba = lineaSticker{texto: "↑ " + strconv.Itoa(desde) + " líneas más arriba", color: rgbApagado}
		}
		if hasta < len(cuerpo) {
			abajo = lineaSticker{texto: "↓ " + strconv.Itoa(len(cuerpo)-hasta) + " líneas más abajo", color: rgbApagado}
		}
		cuerpo = append(append([]lineaSticker{arriba}, cuerpo[desde:hasta]...), abajo)
	}
	return recortarAncho(append(append(cab, cuerpo...), pie...), ancho)
}

const ayudaTablero = "j/k · / filtra · d detalle · s entrega · S todas · r reintenta · q sale"

// margenTablero es el borde transparente del sticker sobre el plasma.
const margenTablero = 2

// recortarAncho corta con "…" las líneas que no caben: un título largo
// ensanchaba el sticker más que la terminal y se perdía el final de todas.
func recortarAncho(ls []lineaSticker, ancho int) []lineaSticker {
	max := ancho - 2*margenTablero
	if ancho <= 0 || max < 1 {
		return ls
	}
	for i, l := range ls {
		if r := []rune(l.texto); len(r) > max {
			ls[i].texto = string(r[:max-1]) + "…"
		}
	}
	return ls
}

// Tipos de acción que el tablero puede devolver al comando.
const (
	AccionEntregar      = "entregar"       // ship --dry-run sobre la tarea
	AccionReintentar    = "reintentar"     // run --reintentar
	AccionEntregarTodas = "entregar_todas" // ship --todas
)

// Accion es lo que el humano pidió desde el tablero. Tipo vacío = salió
// sin pedir nada.
type Accion struct {
	Tipo string
	ID   string
}

// CorrerBoard muestra el tablero sobre un plasma animado; sale con q o esc.
// El tablero no ejecuta nada: devuelve lo que el humano pidió para que el
// comando lo corra en la misma terminal, con su salida normal.
func CorrerBoard(root string) (Accion, error) {
	filas, err := Tablero(root)
	if err != nil {
		return Accion{}, err
	}
	m := boardModel{todas: filas, filas: filas, cursor: cursorInicial(filas), params: DefaultPlasma(), root: root, presupuesto: lineasPresupuesto(root)}
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return Accion{}, err
	}
	return final.(boardModel).accion, nil
}

type boardModel struct {
	// filas es lo que se ve y lo que recorre el cursor; todas, lo que hay
	// en disco. Solo difieren con un filtro puesto.
	filas []Fila
	todas []Fila

	filtro     string
	filtrando  bool // el teclado escribe en el filtro
	confirmaS  bool // ya se pidió S una vez: la segunda entrega
	detalleID  string
	detalle    []lineaSticker
	desplazado int

	cursor      int
	accion      Accion
	aviso       string
	presupuesto []string
	ancho       int
	alto        int
	t           float64
	params      PlasmaParams

	// root y ticks sirven al refresco: el tablero releía disco una sola
	// vez al abrirse, así que una corrida en paralelo avanzaba entera sin
	// que se moviera nada en pantalla.
	root  string
	ticks int
}

// ticksPorRefresco: el plasma late cada 100 ms; releer disco a ese ritmo
// es desperdicio, una vez por segundo alcanza para que se sienta vivo.
const ticksPorRefresco = 10

func (m boardModel) Init() tea.Cmd {
	return tickPlasma()
}

func (m boardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.ancho = msg.Width
		m.alto = msg.Height
		return m, nil
	case tickMsg:
		m.t += 0.1
		m.ticks++
		if m.ticks%ticksPorRefresco == 0 {
			m.refrescar()
		}
		return m, tickPlasma()
	case tea.KeyMsg:
		return m.tecla(msg)
	}
	return m, nil
}

// saltoPagina es cuánto mueven pgup/pgdn.
const saltoPagina = 10

func (m boardModel) tecla(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	tecla := msg.String()
	if tecla == "ctrl+c" {
		return m, tea.Quit
	}
	confirma := m.confirmaS
	m.confirmaS = false

	if m.filtrando {
		switch {
		case tecla == "enter":
			m.filtrando = false
		case tecla == "esc":
			m.filtrando, m.filtro = false, ""
		case tecla == "backspace":
			if r := []rune(m.filtro); len(r) > 0 {
				m.filtro = string(r[:len(r)-1])
			}
		case msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace:
			m.filtro += string(msg.Runes)
		}
		m.filtrar()
		return m, nil
	}

	if m.detalleID != "" {
		switch tecla {
		case "q":
			return m, tea.Quit
		case "d", "esc", "enter":
			m.detalleID, m.detalle, m.desplazado = "", nil, 0
		case "j", "down":
			m.desplazar(1)
		case "k", "up":
			m.desplazar(-1)
		case "pgdown", "ctrl+d":
			m.desplazar(saltoPagina)
		case "pgup", "ctrl+u":
			m.desplazar(-saltoPagina)
		}
		return m, nil
	}

	n := len(m.filas)
	switch tecla {
	case "q":
		return m, tea.Quit
	case "esc":
		// con filtro puesto, esc lo quita; sin filtro, sale
		if m.filtro == "" {
			return m, tea.Quit
		}
		m.filtro = ""
		m.filtrar()
	case "/":
		m.filtrando, m.aviso = true, ""
	case "j", "down":
		if n > 0 {
			m.cursor = (m.cursor + 1) % n
			m.aviso = ""
		}
	case "k", "up":
		if n > 0 {
			m.cursor = (m.cursor - 1 + n) % n
			m.aviso = ""
		}
	case "pgdown", "ctrl+d":
		m.cursor = min(m.cursor+saltoPagina, max(n-1, 0))
	case "pgup", "ctrl+u":
		m.cursor = max(m.cursor-saltoPagina, 0)
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = max(n-1, 0)
	case "d", "enter":
		if id := selectedID(m.filas, m.cursor); id != "" {
			m.detalleID, m.desplazado = id, 0
			m.detalle = detalleTarea(m.root, id, m.ancho)
		}
	case "S":
		listas := 0
		for _, f := range m.base() {
			if f.Estado == state.Lista {
				listas++
			}
		}
		switch {
		case listas == 0:
			m.aviso = "ninguna tarea lista · S entrega las que están en LISTO"
		case !confirma:
			// abre un PR: no sale de una tecla suelta
			m.confirmaS = true
			m.aviso = "S otra vez entrega las " + strconv.Itoa(listas) + " listas en un solo PR · cualquier otra tecla cancela"
		default:
			m.accion = Accion{Tipo: AccionEntregarTodas}
			return m, tea.Quit
		}
	case "s", "r":
		if m.cursor < 0 || m.cursor >= n {
			return m, nil
		}
		accion, aviso := accionDeTecla(tecla, m.filas[m.cursor])
		if aviso != "" {
			m.aviso = aviso
			return m, nil
		}
		m.accion = accion
		return m, tea.Quit
	}
	return m, nil
}

// base son todas las tareas, con o sin filtro puesto.
func (m boardModel) base() []Fila {
	if m.todas != nil {
		return m.todas
	}
	return m.filas
}

// filtrar deja en filas las tareas cuyo id o título contiene el filtro,
// sin distinguir mayúsculas, y mantiene el cursor sobre la misma tarea.
func (m *boardModel) filtrar() {
	sel := selectedID(m.filas, m.cursor)
	todas := m.base()
	m.todas = todas
	m.filas = todas
	if f := strings.ToLower(m.filtro); f != "" {
		m.filas = nil
		for _, x := range todas {
			if strings.Contains(strings.ToLower(x.ID+" "+x.Titulo), f) {
				m.filas = append(m.filas, x)
			}
		}
	}
	m.cursor = 0
	for i, f := range m.filas {
		if f.ID == sel {
			m.cursor = i
		}
	}
}

// desplazar mueve el panel de detalle sin pasarse del final.
func (m *boardModel) desplazar(d int) {
	tope := max(len(m.detalle)-m.altoDetalle(), 0)
	m.desplazado = max(min(m.desplazado+d, tope), 0)
}

// altoDetalle son las líneas del detalle que caben, sin el pie.
func (m boardModel) altoDetalle() int {
	if m.alto <= 0 {
		return len(m.detalle)
	}
	return max(m.alto-2*margenTablero-2, 1)
}

// accionDeTecla traduce una tecla a la acción que el comando ejecutará,
// o a un aviso si la tarea no está en el estado que esa acción necesita.
func accionDeTecla(tecla string, f Fila) (Accion, string) {
	switch tecla {
	case "s":
		if f.Estado != state.Lista {
			return Accion{}, f.ID + " no está lista · s solo entrega las que están en LISTO"
		}
		return Accion{AccionEntregar, f.ID}, ""
	case "r":
		if f.Estado != state.Detenida {
			return Accion{}, f.ID + " no está detenida · r solo revive las que están en DETENIDO"
		}
		return Accion{AccionReintentar, f.ID}, ""
	}
	return Accion{}, ""
}

// refrescar relee el estado de disco sin perder la selección: el cursor
// se sigue por id, no por posición, porque una tarea puede cambiar de
// columna entre dos refrescos.
func (m *boardModel) refrescar() {
	filas, err := Tablero(m.root)
	if err != nil {
		return // un fallo de lectura no debe tumbar el tablero
	}
	m.todas = filas
	m.filtrar()
	m.presupuesto = lineasPresupuesto(m.root)
	if m.detalleID != "" {
		m.detalle = detalleTarea(m.root, m.detalleID, m.ancho)
		m.desplazar(0)
	}
}

func (m boardModel) View() string {
	if m.detalleID != "" {
		hasta := min(m.desplazado+m.altoDetalle(), len(m.detalle))
		ls := append([]lineaSticker{}, m.detalle[min(m.desplazado, hasta):hasta]...)
		pie := "j/k desplaza · d vuelve · q sale"
		if hasta < len(m.detalle) {
			pie = "↓ " + strconv.Itoa(len(m.detalle)-hasta) + " líneas más · " + pie
		}
		ls = append(ls, lineaSticker{}, lineaSticker{texto: pie, color: rgbApagado})
		return FondoPlasma(m.ancho, m.alto, m.t, m.params, recortarAncho(ls, m.ancho), margenTablero)
	}
	filtro := ""
	if m.filtrando {
		filtro = "/" + m.filtro + "▏ enter fija · esc quita"
	} else if m.filtro != "" {
		filtro = "/" + m.filtro + " · " + strconv.Itoa(len(m.filas)) + " de " + strconv.Itoa(len(m.todas)) + " · esc quita"
	}
	return FondoPlasma(m.ancho, m.alto, m.t, m.params, encajarTablero(m.filas, selectedID(m.filas, m.cursor), m.aviso, filtro, m.presupuesto, m.ancho, m.alto), margenTablero)
}
