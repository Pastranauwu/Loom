// Package plan implementa al planificador: convierte una
// petición en lenguaje natural en contratos de tarea. El contrato lo
// redacta un modelo; loom solo parsea, muestra y aprueba. Nunca lo
// escribe a mano el usuario.
package plan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/skills"
	"github.com/Pastranauwu/loom/internal/task"
)

// Borrador es una tarea propuesta por el planificador, antes de asignarle
// id y version.
type Borrador struct {
	Titulo      string   `json:"titulo"`
	Porque      string   `json:"porque"`
	ListoCuando string   `json:"listo_cuando"`
	TocarSolo   []string `json:"tocar_solo"`
	NoTocar     []string `json:"no_tocar"`
	DependeDe   []string `json:"depende_de"`
	Expone      []string `json:"expone"`
	Usa         []string `json:"usa"`
	Riesgos     string   `json:"riesgos"`
	Peso        string   `json:"peso"`
	Agente      string   `json:"agente,omitempty"`
	// Skills son las del catálogo que la tarea necesita: nil si el modelo
	// no lo dijo (quedan las del rol), [] si dijo que ninguna.
	Skills []string `json:"skills"`
	// Cubre son los ids de requerimiento que implementa la tarea.
	Cubre []string `json:"cubre"`
	// LimiteLineas se lee por compatibilidad con planes anteriores.
	// Solo la configuración humana determina el tope efectivo.
	LimiteLineas int `json:"limite_lineas"`

	// Como es el enfoque que sugiere el orquestador: cómo encarar la
	// tarea, qué tocar primero, a qué no meterse. Es orientación para el
	// ejecutor, no contrato vinculante como listo_cuando — se inyecta en
	// el prompt como nota, y en una descomposición recursiva es lo único
	// que le dice al agente chico cómo cumplir su parte sin inventar.
	Como string `json:"como,omitempty"`
}

// UnmarshalJSON tolera que el modelo escriba las dependencias como
// números (`"depende_de": [1]`) o como ids completos (`["T-002"]`):
// `depende_de` es []string en el contrato, y un JSON numérico rompía
// toda la descomposición recursiva.

// ErrSinTareas es un plan vacío. Para el planificador es un error; para
// el arquitecto puede ser la respuesta correcta (el código ya cumple).
var ErrSinTareas = errors.New("el modelo no propuso ninguna tarea")

func (b *Borrador) UnmarshalJSON(data []byte) error {
	type alias Borrador
	var raw struct {
		*alias
		DependeDe json.RawMessage `json:"depende_de"`
	}
	a := alias(*b)
	raw.alias = &a
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*b = Borrador(a)
	if len(raw.DependeDe) == 0 {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw.DependeDe, &items); err != nil {
		return err
	}
	deps := make([]string, 0, len(items))
	for _, it := range items {
		var s string
		if json.Unmarshal(it, &s) == nil {
			deps = append(deps, s)
			continue
		}
		var n float64
		if json.Unmarshal(it, &n) == nil {
			deps = append(deps, strconv.FormatFloat(n, 'f', -1, 64))
			continue
		}
		deps = append(deps, strings.Trim(string(it), `"`))
	}
	b.DependeDe = deps
	return nil
}

// Generador pide texto a un modelo. El comando lo adapta desde el
// ejecutor; aquí solo importa la interfaz mínima.
type Generador interface {
	Generar(ctx context.Context, prompt string) (string, error)
}

// Contexto es lo que loom sabe del repositorio al momento de
// planear. Se lo pasa al modelo para que no adivine el stack ni
// invente comandos que no existen en el proyecto.
type Contexto struct {
	Lenguaje     string   // go, node, python, rust, "" si no se detecta
	EsVacio      bool     // repo sin código fuente todavía
	Pruebas      string   // comando de pruebas detectado ("" si no hay)
	Stack        string   // stack elegido por el humano ("" si lo decide el modelo)
	Requisitos   string   // requisitos extra que dijo el humano, en texto libre
	Constitucion string   // contenido de .loom/constitution.md, "" si no existe
	Vedadas      []string // globs que tocar_solo nunca puede incluir (zonas prohibidas + rutas de prueba)
	// Ocupados son los alcances que ya tienen dueño: tocar_solo de las
	// tareas activas, por id. Sin esto el planificador propone tareas
	// que se cruzan con las que ya corren, la esclusa de entrada las
	// rechaza por solapamiento y los tokens del plan se gastaron para nada.
	Ocupados map[string][]string
	// Expuestas son las firmas que ya prometen las tareas del repo, por
	// id. ValidatePlan compara el plan contra ellas: sin verlas, el
	// modelo reescribe "get_db() -> Iterator[Session]" como
	// "def get_db() -> Generator[Session]" y el plan entero se rechaza.
	Expuestas map[string][]string
	// PrimerID es el id que recibirá la primera tarea del plan. Sin
	// saberlo, el modelo que ve tareas previas numera por su cuenta
	// (T-002 después de T-001) y leer eso por posición armaba ciclos.
	PrimerID string
	Agentes  map[string]config.Agente // agentes disponibles en config.yml
	// PruebasPropias marca que este stack no tiene examinador ciego, así
	// que las pruebas las escribe la propia tarea y su archivo tiene que
	// entrar en tocar_solo. Sin decirlo, el planificador apunta el
	// listo_cuando a un archivo de prueba que deja fuera de alcance, y
	// nadie puede crearlo: la tarea queda roja para siempre.
	PruebasPropias bool
	// Skills es el catálogo de .agents/skills del que el planificador
	// elige las de cada tarea. Vacío: el campo no se pide.
	Skills []skills.Skill
}

// Prompt pide decisiones de arquitectura y contratos en una sola llamada.
// El contexto del repo se inyecta para
// que el modelo use el lenguaje y el comando de pruebas reales.
func Prompt(frase string, c Contexto) string {
	return prompt("Eres el planificador de loom. Diseña la solución y reparte unidades de trabajo completas y verificables:\n\n", "\""+frase+"\"\n\n", c)
}

// PromptCompletar pide el contrato de tareas que el humano ya decidió en
// un spec rápido. El planificador no reparte: escribe listo_cuando,
// alcance, dependencias y firmas de exactamente esas tareas, en ese orden.
func PromptCompletar(feature string, reglas []string, tareas []task.Task, c Contexto) string {
	n := len(tareas)
	intro := fmt.Sprintf("Eres el planificador de loom. El humano ya decidió estas %d tareas; tu trabajo es escribir su contrato. "+
		"NO partas, juntes, quites ni agregues tareas: devuelve exactamente %d, en este mismo orden y con el mismo \"titulo\". "+
		"Si una tarea ya trae un campo, cópialo tal cual.\n\n", n, n)

	var p strings.Builder
	if feature != "" {
		fmt.Fprintf(&p, "Feature: %s\n", feature)
	}
	for _, r := range reglas {
		fmt.Fprintf(&p, "Regla para todas: %s\n", r)
	}
	p.WriteString("Tareas:\n")
	for i, t := range tareas {
		fmt.Fprintf(&p, "%d. %s · %s", i+1, t.ID, t.Titulo)
		if t.ListoCuando != "" {
			fmt.Fprintf(&p, " · listo_cuando: %s", t.ListoCuando)
		}
		if len(t.TocarSolo) > 0 {
			fmt.Fprintf(&p, " · tocar_solo: %s", strings.Join(t.TocarSolo, ", "))
		}
		if len(t.DependeDe) > 0 {
			fmt.Fprintf(&p, " · depende_de: %s", strings.Join(t.DependeDe, ", "))
		}
		if t.Notas != "" {
			fmt.Fprintf(&p, " · notas: %s", t.Notas)
		}
		p.WriteString("\n")
	}
	if n > 0 {
		fmt.Fprintf(&p, "En \"depende_de\" usa los ids de esta lista (ej. \"%s\").\n\n", tareas[0].ID)
	}
	return prompt(intro, p.String(), c)
}

// prompt arma la instrucción común a planear y a completar: qué se pide,
// el contexto del repo y el formato de cada contrato.
func prompt(intro, peticion string, c Contexto) string {
	var b strings.Builder
	b.WriteString(intro)
	if c.Constitucion != "" {
		b.WriteString("Constitución del proyecto (convenciones establecidas que el plan debe respetar):\n")
		b.WriteString(c.Constitucion)
		b.WriteString("\n\n")
	}
	b.WriteString(peticion)
	b.WriteString(contextoPrompt(c))
	b.WriteString("\n\nEres el ARQUITECTO de la solución, no un implementador: no escribes código. Tu trabajo es dejar la arquitectura definida —estilo, árbol de archivos, responsabilidades de cada archivo, flujo de datos, tipos compartidos y la firma exacta de cada función pública (parámetros y valor de retorno)— para que cada ejecutor solo escriba los archivos que le tocan sin rediseñar nada. Aplica SOLID donde aporte; usa arquitectura hexagonal o microservicios solo cuando el problema lo justifique.\n")
	b.WriteString("Devuelve SOLO un objeto JSON con \"arquitectura\" (texto con el árbol de archivos, sus responsabilidades, tipos compartidos y firmas exactas) y \"tareas\" (array de contratos). La arquitectura se guarda en las notas de todos los contratos.\n")
	b.WriteString("Organiza \"arquitectura\" en párrafos separados por una línea en blanco, cada uno con su encabezado en mayúsculas (STACK:, ESTILO:, TIPOS COMPARTIDOS:, ...). Dos son obligatorios y con formato fijo, porque a cada ejecutor le llega solo la parte que le toca: \"ÁRBOL DE ARCHIVOS:\" con una línea por archivo que empiece por su ruta (\" src/a.js (T-001) responsabilidad\"), y \"FIRMAS PÚBLICAS EXACTAS:\" con un bloque por archivo que empiece por \"[T-00N ruta]\" seguido de sus firmas.\n")
	b.WriteString("Divide y vencerás: parte en tareas por ARCHIVO o MÓDULO, cada una con su \"tocar_solo\" acotado a los archivos que escribe, su \"expone\"/\"usa\" con las firmas exactas (parámetros y retorno) y un \"como\" que diga exactamente qué escribir. Cuantas más tareas pequeñas e independientes, más modelos corren en paralelo y más rápido el resultado: preferí varias tareas livianas a una sola tarea grande. Si hace falta una base compartida (tipos, contratos), asígnala a una tarea pesada con agente architect y haz que sus consumidoras dependan de ella. Reserva UNA tarea final para la composición y sus pruebas de integración.\n")
	b.WriteString("Cada tarea contiene estos campos:\n")
	b.WriteString("- \"titulo\": frase corta en minúscula\n")
	b.WriteString("- \"porque\": por qué importa (una frase)\n")
	b.WriteString("- \"listo_cuando\": un comando ejecutable que HOY FALLE y que pase cuando la tarea esté hecha (obligatorio).\n")
	b.WriteString("  · loom lo ejecuta ANTES de gastar un solo token y rechaza la tarea si ya pasa: un comando que hoy da verde no verifica nada.\n")
	b.WriteString("  · Por eso no sirve el comando de pruebas del proyecto tal cual si la suite ya está verde (`npm test`, `go test ./...`, `pytest`): acótalo a lo que esta tarea va a crear — el archivo de prueba, el módulo o el paquete que todavía no existe.\n")
	b.WriteString("  · Ejemplos que fallan hoy porque el destino no existe: \"go test ./internal/wol/...\", \"node --test test/validator.test.js\", \"pytest tests/test_wol.py\", \"npm test -- validator\".\n")
	b.WriteString("- \"tocar_solo\": array de globs de archivos que la tarea puede tocar\n")
	b.WriteString("  · dos tareas NUNCA comparten un archivo en \"tocar_solo\": corren en paralelo y chocan al integrar. Si varias necesitan el mismo (go.mod, package.json, un router), lo toca solo una y las demás la ponen en \"depende_de\".\n")
	if c.PruebasPropias {
		b.WriteString("  · en este proyecto las pruebas las escribe la propia tarea: si el \"listo_cuando\" apunta a un archivo de prueba, ESE archivo tiene que estar también en \"tocar_solo\", o nadie podrá crearlo.\n")
	}
	if len(c.Vedadas) > 0 {
		b.WriteString("  · \"tocar_solo\" NUNCA puede incluir estas rutas (las maneja el proyecto o el examinador ciego, no la tarea): " + strings.Join(c.Vedadas, ", ") + "\n")
	}
	if len(c.Ocupados) > 0 {
		b.WriteString("  · estas rutas YA las está tocando otra tarea en curso; \"tocar_solo\" no puede cruzarse con ellas o la tarea será rechazada sin llegar a ejecutarse:\n")
		for _, id := range idsOrdenados(c.Ocupados) {
			b.WriteString("      " + id + ": " + strings.Join(c.Ocupados[id], ", ") + "\n")
		}
	}
	b.WriteString("- \"depende_de\": array de ids (ej. \"T-001\") de tareas que deben estar verdes antes que esta; vacío si no depende de ninguna\n")
	if c.PrimerID != "" && c.PrimerID != "T-001" {
		b.WriteString("  · ya hay tareas en el repo: tus tareas reciben ids correlativos desde " + c.PrimerID + " en el orden del array. Usa esos ids en \"depende_de\", en el ÁRBOL y en las FIRMAS; para depender de una tarea previa usa su id.\n")
	}
	b.WriteString("- \"expone\": array de firmas públicas EXACTAS que esta tarea produce y otra consume, con tipos de entrada y de retorno (ej. \"wol.Send(mac string, addr string) error\", \"POST /wake\"); vacío si no produce ninguna\n")
	if len(c.Expuestas) > 0 {
		b.WriteString("  · firmas que ya exponen tareas previas; si las consumes, cópialas en \"usa\" palabra por palabra y pon su id en \"depende_de\" (nunca las vuelvas a exponer):\n")
		for _, id := range idsOrdenados(c.Expuestas) {
			b.WriteString("      " + id + ": " + strings.Join(c.Expuestas[id], " · ") + "\n")
		}
	}
	b.WriteString("- \"usa\": array de firmas de OTRAS tareas que esta consume, copiadas palabra por palabra del \"expone\" de aquella (mismo nombre, mismos tipos); vacío si no consume ninguna. Si no puedes copiar la firma exacta, la tarea está mal partida: resuelve la dependencia en el plan, no en el ejecutor\n")
	b.WriteString("- \"peso\": \"liviana\", \"media\" o \"pesada\" según la complejidad: usa liviana para implementación con decisiones ya resueltas, media para lógica compleja y pesada para base arquitectónica o incertidumbre alta\n")
	b.WriteString("- \"limite_lineas\": 0. No impongas un máximo de líneas ni dividas por tamaño: divide por responsabilidad y dependencias. Los límites positivos solo los decide el humano.\n")
	if len(c.Agentes) > 0 {
		var ags []string
		for nombre, a := range c.Agentes {
			desc := nombre
			if len(a.Skills) > 0 {
				desc += fmt.Sprintf(" (habilidades: %s)", strings.Join(a.Skills, ", "))
			}
			ags = append(ags, desc)
		}
		sort.Strings(ags)
		fmt.Fprintf(&b, "- \"agente\": nombre del agente asignado para esta tarea (disponibles: %s); o \"\" para el ejecutor por defecto\n", strings.Join(ags, "; "))
	}
	if len(c.Skills) > 0 {
		b.WriteString("- \"skills\": array con las skills que ESTA tarea necesita, de este catálogo; su texto entero entra al prompt del ejecutor, así que cada una cuesta tokens en cada intento. Pon solo las que cambian cómo se escribe esta tarea (p. ej. diseño visual solo en tareas que dibujan interfaz) y [] si ninguna hace falta:\n")
		for _, sk := range c.Skills {
			fmt.Fprintf(&b, "  - %s: %s\n", sk.Nombre, recortar(sk.Descripcion, 200))
		}
	}
	b.WriteString("- \"riesgos\": riesgos o limitaciones, o \"\" si no hay\n")
	b.WriteString("- \"como\": instrucciones suficientes para ejecutar sin rediseñar: archivos concretos, pasos, entradas y salidas, errores, casos límite, dependencias y pruebas que cubran los requisitos. No lo limites a una línea.\n\n")
	b.WriteString("Las tareas corren en paralelo y aisladas: no pueden leerse el código entre sí. Si una produce algo que otra necesita, la firma DEBE aparecer igual en el \"expone\" de la que la produce y en el \"usa\" de la que la consume; si no, cada una inventará la suya y no van a encajar.\n\n")
	b.WriteString("Y no prometas de más: lo que una tarea soporta y su consumidora nunca pide no lo prueba nadie, aunque las dos queden verdes. Si una pieza tiene que aceptar un operador, un formato o un caso límite, el \"listo_cuando\" de la tarea que lo consume tiene que cubrirlo, o no lo declares en el \"expone\" de la que lo produce.\n\n")
	b.WriteString("Ejemplo:\n{\n  \"arquitectura\": \"Módulo Go existente. internal/wol/send.go construye y envía el paquete UDP; internal/api/wake.go adapta HTTP a wol.Send. El handler compone la llamada sin duplicar lógica UDP. Cada módulo conserva sus pruebas junto al código.\",\n  \"tareas\": [\n    {\n      \"titulo\": \"enviar magic packet\",\n      \"porque\": \"es la acción central\",\n      \"listo_cuando\": \"go test ./internal/wol/...\",\n      \"tocar_solo\": [\n        \"internal/wol/**\"\n      ],\n      \"expone\": [\n        \"wol.Send(mac, addr string) error\"\n      ],\n      \"usa\": [],\n      \"depende_de\": [],\n      \"peso\": \"liviana\",\n      \"limite_lineas\": 0,\n      \"riesgos\": \"\",\n      \"como\": \"Implementa Send en internal/wol/send.go: valida MAC, construye 6 bytes FF más 16 repeticiones de la MAC, envía UDP a addr y propaga errores. Verifica paquete exacto, MAC inválida y fallo de envío.\"\n    },\n    {\n      \"titulo\": \"endpoint http que dispara wol\",\n      \"porque\": \"lo invoca la automatización\",\n      \"listo_cuando\": \"go test ./internal/api/...\",\n      \"tocar_solo\": [\n        \"internal/api/**\"\n      ],\n      \"expone\": [\n        \"POST /wake\"\n      ],\n      \"usa\": [\n        \"wol.Send(mac, addr string) error\"\n      ],\n      \"depende_de\": [\n        \"T-001\"\n      ],\n      \"peso\": \"liviana\",\n      \"limite_lineas\": 0,\n      \"riesgos\": \"\",\n      \"como\": \"Crea y registra POST /wake en internal/api/wake.go usando el router existente en ese alcance. Lee JSON mac y addr, llama wol.Send, responde 204 al éxito, 400 para entrada inválida y 502 si falla UDP. Verifica respuestas y que la petición válida invoque el envío.\"\n    }\n  ]\n}")
	return b.String()
}

// contextoPrompt redacta la sección de contexto según lo detectado en
// el repo. En un repo vacío activa el modo greenfield: la primera
// tarea inicializa el stack y fija su comando de verificación real.
func contextoPrompt(c Contexto) string {
	var b strings.Builder
	if c.EsVacio {
		b.WriteString("El repositorio está vacío (sin código todavía). Debes arrancar desde cero:\n")
		if c.Stack != "" {
			fmt.Fprintf(&b, "- El humano eligió el stack: %s. Úsalo para todas las tareas.\n", c.Stack)
		} else {
			b.WriteString("- Elige tú UN stack (Go, Node, Python, ...) y fija su estructura. La PRIMERA tarea lo inicializa siguiendo esas decisiones y deja el proyecto compilando.\n")
		}
		b.WriteString("- La primera tarea deja el proyecto compilando; su \"listo_cuando\" es el comando de build o test real del stack (ej. \"go build ./...\" si Go, \"npm test\" si Node).\n")
		b.WriteString("- Las demás tareas construyen sobre esa base, con \"listo_cuando\" reales de ese mismo stack que hoy fallen, y marcan en \"depende_de\" la tarea que creó la base (y cualquier otra de la que dependan).\n")
		b.WriteString("- Mantén UN solo lenguaje en todas las tareas.\n")
		if c.Requisitos != "" {
			fmt.Fprintf(&b, "- Además de la petición, el humano pidió: %s\n", c.Requisitos)
		}
		return b.String()
	}
	if c.Lenguaje == "" {
		return "El repositorio ya existe pero no se detectó un lenguaje concreto. Inspéctalo antes de elegir comandos y usa el comando de pruebas que ya exista, si lo hay."
	}
	pruebas := c.Pruebas
	if strings.TrimSpace(pruebas) == "" {
		pruebas = "ninguno detectado · propón uno que exista o el estándar del lenguaje"
	}
	return fmt.Sprintf("El repositorio ya está en desarrollo. Lenguaje detectado: %s. Comando de pruebas del proyecto: %s.\n"+
		"- Mantén ese lenguaje y ese corredor de pruebas; no cambies el stack ni propongas paquetes o rutas que no existen.\n"+
		"- Pero NO copies ese comando tal cual en los \"listo_cuando\" si la suite del proyecto ya está verde: entonces pasaría hoy y loom rechazaría la tarea. Apunta con el mismo corredor a lo que la tarea va a crear y que todavía no existe.",
		c.Lenguaje, pruebas)
}

// Parse extrae la lista de borradores de la respuesta del modelo, tolerando
// vallas markdown y texto alrededor. Un borrador sin titulo ni listo_cuando
// se rechaza: un plan sin criterio de "listo" no es un plan.
func Parse(texto string) ([]Borrador, error) {
	// Conserva compatibilidad con el array de contratos de versiones anteriores.
	t := CuerpoJSON(texto)
	if t == "" {
		return nil, errors.New("el modelo no devolvió un plan JSON · vuelve a intentarlo")
	}
	ini := 0
	var bs []Borrador
	var arquitectura string
	dec := json.NewDecoder(strings.NewReader(EscaparControles(t)))
	if t[ini] == '{' {
		var documento struct {
			Arquitectura string     `json:"arquitectura"`
			Tareas       []Borrador `json:"tareas"`
		}
		if err := dec.Decode(&documento); err != nil {
			return nil, fmt.Errorf("el modelo devolvió JSON inválido · %s", err)
		}
		if strings.TrimSpace(documento.Arquitectura) == "" {
			return nil, errors.New("el plan no define arquitectura · incluye decisiones y árbol de archivos")
		}
		bs, arquitectura = documento.Tareas, documento.Arquitectura
	} else if err := dec.Decode(&bs); err != nil {
		return nil, fmt.Errorf("el modelo devolvió JSON inválido · %s", err)
	}
	for i := range bs {
		if arquitectura != "" {
			if strings.TrimSpace(bs[i].Como) == "" {
				return nil, fmt.Errorf("la tarea %d no trae instrucciones de implementación en como", i+1)
			}
			bs[i].Como = MarcaArquitectura + arquitectura + "\n\n" + MarcaImplementacion + bs[i].Como
		}
	}

	if len(bs) == 0 {
		return nil, ErrSinTareas
	}
	for i, b := range bs {
		if strings.TrimSpace(b.Titulo) == "" || strings.TrimSpace(b.ListoCuando) == "" {
			return nil, fmt.Errorf("la tarea %d del plan no trae titulo ni listo_cuando · el modelo se desvió", i+1)
		}
	}
	return bs, nil
}

// CuerpoJSON devuelve el valor JSON más largo que hay en la respuesta de
// un modelo, desde su primer carácter. Tomar el primer "{" del texto tiraba
// la respuesta cuando el modelo narraba antes de responder y la prosa traía
// llaves ("{campo}" en un párrafo): una ronda de corrección pagada por un
// plan que estaba bien. Si nada decodifica, devuelve desde el
// primer "{" o "[" para que el error del decodificador sea el del plan.
func CuerpoJSON(texto string) string {
	mejor, primero, largo := "", "", 0
	for i := 0; i < len(texto); i++ {
		if texto[i] != '{' && texto[i] != '[' {
			continue
		}
		if primero == "" {
			primero = texto[i:]
		}
		var crudo json.RawMessage
		dec := json.NewDecoder(strings.NewReader(EscaparControles(texto[i:])))
		if dec.Decode(&crudo) != nil {
			continue
		}
		if len(crudo) > largo {
			mejor, largo = texto[i:], len(crudo)
		}
		// lo de adentro de un valor que ya decodificó es más corto
		i += int(dec.InputOffset()) - 1
	}
	if mejor == "" {
		return primero
	}
	return mejor
}

// EscaparControles escapa saltos de línea y tabuladores crudos dentro de
// strings JSON. Los modelos escriben "arquitectura" con párrafos reales
// en vez de \n, y el decoder rechazaba un plan de 49k tokens entero.
func EscaparControles(s string) string {
	var b strings.Builder
	enString, escape := false, false
	for _, r := range s {
		switch {
		case escape:
			escape = false
		case enString && r == '\\':
			escape = true
		case r == '"':
			enString = !enString
		case enString && r == '\n':
			b.WriteString(`\n`)
			continue
		case enString && r == '\r':
			b.WriteString(`\r`)
			continue
		case enString && r == '\t':
			b.WriteString(`\t`)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Generar pide el plan y lo parsea.
func Generar(ctx context.Context, g Generador, c Contexto, frase string) ([]Borrador, error) {
	texto, err := g.Generar(ctx, Prompt(frase, c))
	if err != nil {
		return nil, err
	}
	return Parse(texto)
}

// Completar pide los contratos de las tareas de un spec rápido y exige
// uno por tarea: un modelo que reparte o se salta una desalinea todo lo
// que viene después.
func Completar(ctx context.Context, g Generador, c Contexto, feature string, reglas []string, tareas []task.Task) ([]Borrador, error) {
	texto, err := g.Generar(ctx, PromptCompletar(feature, reglas, tareas, c))
	if err != nil {
		return nil, err
	}
	bs, err := Parse(texto)
	if err != nil {
		return nil, err
	}
	if len(bs) != len(tareas) {
		return nil, fmt.Errorf("el modelo devolvió %d contratos para %d tareas · vuelve a intentarlo", len(bs), len(tareas))
	}
	return bs, nil
}

// AcotarLimiteLineas conserva el límite elegido por el humano. La estimación
// del modelo no crea ni modifica una restricción de entrega; 0 es sin tope.
func AcotarLimiteLineas(propuesto, porDefecto int) int {
	return porDefecto
}

// idsOrdenados devuelve las claves en orden estable: el prompt tiene que
// ser reproducible para que la caché del proveedor sirva de algo.
func idsOrdenados(m map[string][]string) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// recortar acota s a max runas, para que una descripción larga de skill
// no infle el prompt del planificador.
func recortar(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "…"
}
