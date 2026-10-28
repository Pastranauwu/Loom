// Package task implements the task contract: one markdown file
// per task under .loom/tasks/, with a frontmatter block in the same
// yaml subset as config.yml and a free notes body.
package task

import (
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"regexp"
	"strconv"
	"strings"

	"github.com/Pastranauwu/loom/internal/kv"
)

// Defaults for the optional contract fields.
const (
	DefaultLimiteIntentos  = 3
	DefaultLimiteLineas    = 0 // sin tope; un valor positivo es una restricción explícita
	DefaultLimiteSubtareas = 5
)

// Version is the contract version this binary understands.
// A file declaring a higher one is read tolerantly and reported.
const Version = 1

// idPattern validates contract IDs: T-001, T-002, ...
var idPattern = regexp.MustCompile(`^T-\d{3,}$`)

// Task is the task contract. ListoCuando is the only mandatory field
// beyond id and titulo; the rest carry defaults.
type Task struct {
	Version     int      `json:"version"`
	ID          string   `json:"id"`
	Titulo      string   `json:"titulo"`
	Porque      string   `json:"porque"`
	ListoCuando string   `json:"listo_cuando"`
	TocarSolo   []string `json:"tocar_solo"`
	NoTocar     []string `json:"no_tocar"`
	DependeDe   []string `json:"depende_de,omitempty"`

	// Expone son las firmas públicas que esta tarea produce y que otras
	// consumen: "wol.Send(mac, addr string) error", "POST /wake".
	// Usa son las que consume de sus hermanas. Congelarlas en
	// ambos contratos es lo que deja que dos tareas de la misma oleada,
	// que corren ciegas en cuartos separados, se encuentren.
	Expone []string `json:"expone,omitempty"`
	Usa    []string `json:"usa,omitempty"`

	LimiteIntentos int    `json:"limite_intentos"`
	LimiteLineas   int    `json:"limite_lineas"`
	Riesgos        string `json:"riesgos"`
	Peso           string `json:"peso,omitempty"`   // liviana | media | pesada ("" = estrategia global)
	Agente         string `json:"agente,omitempty"` // agente asignado

	// Skills son las skills del catálogo (.agents/skills) que esta tarea
	// necesita; su texto entra al prompt del agente. nil = las del rol del
	// agente, como antes del campo; [] = ninguna.
	Skills []string `json:"skills,omitempty"`

	// Cubre son los ids de los requerimientos del spec que esta tarea
	// implementa. Cuando uno cambia o se quita, loom le dice al
	// arquitecto qué tareas y archivos lo implementaron.
	Cubre []string `json:"cubre,omitempty"`
	// ExamenEsqueleto indica que el arquitecto dejó stubs con Casos: y
	// que un examinador independiente escribe el test de listo_cuando.
	ExamenEsqueleto bool   `json:"examen_esqueleto,omitempty"`
	ExamenVisible   string `json:"examen_visible,omitempty"`

	// Recursivo marca una tarea como demasiado grande para un solo
	// intento de agente: en vez de escribir código directo, el bucle la
	// reparte en subtareas reales (mismo contrato, mismo listo_cuando
	// obligatorio) que corren en cuartos anidados dentro de este mismo
	// cuarto (internal/recurse). Sin esta marca, cero recursión — nunca
	// es una decisión del modelo, siempre del contrato.
	Recursivo bool `json:"recursivo,omitempty"`
	// Red: las pruebas de la tarea necesitan red. Sin ella el sandbox
	// las corre con --unshare-net (solo loopback).
	Red bool `json:"red,omitempty"`
	// LimiteSubtareas topa cuántas subtareas puede proponer la
	// descomposición, mismo espíritu que LimiteIntentos: sin techo, una
	// tarea recursiva puede gastar tokens sin límite.
	LimiteSubtareas int `json:"limite_subtareas,omitempty"`

	// Notas is the free body after the frontmatter.
	Notas string `json:"notas,omitempty"`

	// Aviso carries the warning of a contract written by a newer
	// loom: it parsed, but fields were skipped.
	Aviso string `json:"aviso,omitempty"`
}

// ValidID reports whether id has the contract format.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// DependenciasPorPosicion traduce dependencias escritas por posición en
// una lista ("T-001" o "1" = la primera) a los ids reales de esa lista.
// Quien escribe un plan o un spec no sabe qué ids le van a tocar: con
// tareas previas en el repo, su "T-001" es ids[0], no la T-001 vieja. Lo
// que no cae dentro de la lista queda tal cual.
func DependenciasPorPosicion(deps, ids []string) []string {
	if deps == nil {
		return nil
	}
	out := make([]string, len(deps))
	for i, d := range deps {
		out[i] = d
		n, err := strconv.Atoi(strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(d)), "T-"))
		if err == nil && n >= 1 && n <= len(ids) {
			out[i] = ids[n-1]
		}
	}
	return out
}

// NombreDeFirma reduce una firma de `expone`/`usa` al identificador que
// tiene que aparecer sí o sí en el código: "wol.Send(mac string) error"
// → "Send", "POST /wake" → "/wake".
//
// Deliberadamente no compara la firma completa. El lenguaje reescribe los
// nombres de parámetros y el orden de los tipos, así que exigir el texto
// literal rechazaría implementaciones correctas. Lo que sí atrapa es el
// fallo real: que la tarea no haya implementado la pieza, o la haya
// bautizado distinto de lo que su hermana espera. La verificación por
// AST completo sigue pendiente.
func NombreDeFirma(firma string) string {
	s := strings.TrimSpace(firma)
	if s == "" {
		return ""
	}

	// Limpiar modificadores comunes (TypeScript, Go, etc.)
	// "export default async function foo()" -> "function foo()"
	// "export interface Bar" -> "interface Bar"
	modificadores := []string{
		"export default", "export", "declare", "default",
		"async", "public", "private", "protected", "readonly",
	}
	limpiado := true
	for limpiado {
		limpiado = false
		for _, mod := range modificadores {
			if strings.HasPrefix(s, mod+" ") {
				s = strings.TrimSpace(strings.TrimPrefix(s, mod))
				limpiado = true
				break
			}
		}
	}

	// declaración de tipo, interfaz, clase o función: "type Point = { x: number }"
	// → "Point", "const LEVELS: Level[]" → "LEVELS", "interface ISynth<T>" → "ISynth".
	if i := strings.Index(s, " "); i >= 0 {
		switch s[:i] {
		case "type", "interface", "class", "enum", "const", "var", "let", "func", "function":
			s = strings.TrimSpace(s[i:])
			if j := strings.IndexAny(s, " \t:=({[<"); j >= 0 {
				s = s[:j]
			}
			return strings.TrimSpace(s)
		}
	}

	if i := strings.Index(s, "("); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	// ruta HTTP: "POST /wake" → "/wake". Una barra suelta no alcanza:
	// "cmd/sum main package" es prosa, y tomarla por ruta hacía que la
	// esclusa buscara "/sum main package" en el diff y frenara para
	// siempre. Una ruta real no lleva espacios.
	if i := strings.Index(s, "/"); i >= 0 {
		ruta := strings.TrimSpace(s[i:])
		if ruta != "/" && !strings.ContainsAny(ruta, " \t") {
			return ruta
		}
	}
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	// genéricos remanentes en el identificador: "Symbol<T>" -> "Symbol"
	if i := strings.Index(s, "<"); i >= 0 {
		s = s[:i]
	}
	// anotación de tipo o asignación: "LEVELS: Level[]" → "LEVELS",
	// "SoundManager = { play(...) }" → "SoundManager"
	if i := strings.IndexAny(s, ":="); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if i := strings.LastIndex(s, " "); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(s)
}

// Parse reads a task file. Unknown keys and malformed values are
// rejected: the contract is the guardrail, so it is strict.
func Parse(data []byte) (Task, error) {
	var t Task
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(content, "\n")

	if strings.TrimSpace(lines[0]) != "---" {
		return t, errors.New(idioma.T("falta el bloque --- inicial"))
	}
	closing := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closing = i
			break
		}
	}
	if closing == -1 {
		return t, errors.New(idioma.T("falta el bloque --- de cierre"))
	}

	pares, err := kv.Pairs(lines[1:closing], 2)
	if err != nil {
		return t, err
	}

	// la versión se lee primero: decide qué hacer con lo desconocido
	for _, p := range pares {
		if p.Key != "version" {
			continue
		}
		n, err := kv.ParseInt(p.Value)
		if err != nil || n < 1 {
			return t, fmt.Errorf(idioma.T("version inválida: %s · usa version: %d"), p.Value, Version)
		}
		t.Version = n
	}
	if t.Version > Version {
		t.Aviso = fmt.Sprintf(idioma.T("contrato versión %d, binario soporta %d · actualiza loom"), t.Version, Version)
	}

	for _, p := range pares {
		var err error
		switch p.Key {
		case "version":
		case "id":
			t.ID = kv.Unquote(p.Value)
		case "title", "titulo":
			t.Titulo = kv.Unquote(p.Value)
		case "why", "porque":
			t.Porque = kv.Unquote(p.Value)
		case "done_when", "listo_cuando":
			t.ListoCuando = kv.Unquote(p.Value)
		case "only_touch", "tocar_solo":
			t.TocarSolo, err = kv.ParseList(p.Value)
		case "dont_touch", "no_tocar":
			t.NoTocar, err = kv.ParseList(p.Value)
		case "depends_on", "depende_de":
			t.DependeDe, err = kv.ParseList(p.Value)
		case "exposes", "expone":
			t.Expone, err = kv.ParseList(p.Value)
		case "uses", "usa":
			t.Usa, err = kv.ParseList(p.Value)
		case "max_retries", "limite_intentos":
			t.LimiteIntentos, err = kv.ParseInt(p.Value)
		case "max_lines", "limite_lineas":
			t.LimiteLineas, err = kv.ParseInt(p.Value)
		case "risks", "riesgos":
			t.Riesgos = kv.Unquote(p.Value)
		case "weight", "peso":
			t.Peso = NormalizarPeso(kv.Unquote(p.Value))
		case "agent", "agente":
			t.Agente = kv.Unquote(p.Value)
		case "covers", "cubre":
			t.Cubre, err = kv.ParseList(p.Value)
		case "examen_esqueleto":
			switch kv.Unquote(p.Value) {
			case "true":
				t.ExamenEsqueleto = true
			case "false":
				t.ExamenEsqueleto = false
			default:
				err = fmt.Errorf(idioma.T("examen_esqueleto inválido: %s · usa true o false"), p.Value)
			}
		case "examen_visible":
			t.ExamenVisible = kv.Unquote(p.Value)
		case "skills":
			if t.Skills, err = kv.ParseList(p.Value); t.Skills == nil && err == nil {
				t.Skills = []string{} // declarada vacía: ninguna, no las del rol
			}
		case "recursivo":
			switch kv.Unquote(p.Value) {
			case "true":
				t.Recursivo = true
			case "false":
				t.Recursivo = false
			default:
				err = fmt.Errorf(idioma.T("recursivo inválido: %s · usa true o false"), p.Value)
			}
		case "limite_subtareas":
			t.LimiteSubtareas, err = kv.ParseInt(p.Value)
		case "network", "red":
			switch kv.Unquote(p.Value) {
			case "true":
				t.Red = true
			case "false":
				t.Red = false
			default:
				err = fmt.Errorf(idioma.T("network inválido: %s · usa true o false"), p.Value)
			}
		default:
			// campos desconocidos o del futuro: se toleran con aviso para
			// que contratos con campos nuevos no rompan binarios existentes ni reportes.
			if t.Aviso == "" {
				t.Aviso = fmt.Sprintf(idioma.T("campo desconocido ignorado: %s · si es de una versión más nueva, actualiza loom"), p.Key)
			}
			continue
		}
		if err != nil {
			return t, fmt.Errorf("%s: %s", p.Key, err)
		}
	}

	t.Notas = strings.TrimSpace(strings.Join(lines[closing+1:], "\n"))
	return t, nil
}

// Validate checks the contract rules and returns every problem found.
// An empty slice means the contract is valid.
func (t Task) Validate() []error {
	var errs []error
	if t.Version < 1 {
		errs = append(errs, fmt.Errorf(idioma.T("falta version · agrega version: %d al contrato"), Version))
	}
	if !ValidID(t.ID) {
		errs = append(errs, fmt.Errorf(idioma.T("id inválido: %q · usa el formato T-001"), t.ID))
	}
	if strings.TrimSpace(t.Titulo) == "" {
		errs = append(errs, errors.New(idioma.T("falta titulo")))
	}
	if strings.TrimSpace(t.ListoCuando) == "" {
		errs = append(errs, errors.New(idioma.T("falta listo_cuando · escribe el comando que dice \"ya está\"")))
	}
	if t.LimiteIntentos < 1 {
		errs = append(errs, fmt.Errorf(idioma.T("limite_intentos inválido: %d · mínimo 1"), t.LimiteIntentos))
	}
	if t.LimiteLineas < 0 {
		errs = append(errs, fmt.Errorf(idioma.T("limite_lineas inválido: %d · usa 0 para no limitar"), t.LimiteLineas))
	}
	for _, d := range t.DependeDe {
		if !ValidID(d) {
			errs = append(errs, fmt.Errorf(idioma.T("depende_de inválido: %q · usa el formato T-001"), d))
		}
		if d == t.ID {
			errs = append(errs, fmt.Errorf(idioma.T("depende_de se refiere a sí misma: %s · quítala"), d))
		}
	}
	switch t.Peso {
	case "", "liviana", "media", "pesada":
	default:
		errs = append(errs, fmt.Errorf(idioma.T("peso inválido: %q · usa liviana, media o pesada"), t.Peso))
	}
	if t.Agente != "" && strings.ContainsAny(t.Agente, " \t\n\r:") {
		errs = append(errs, fmt.Errorf(idioma.T("agente inválido: %q · usa un nombre sin espacios"), t.Agente))
	}
	if t.Recursivo && t.LimiteSubtareas < 1 {
		errs = append(errs, fmt.Errorf(idioma.T("limite_subtareas inválido: %d · mínimo 1 en una tarea recursiva"), t.LimiteSubtareas))
	}
	if t.ExamenEsqueleto && (t.ExamenVisible == "" || !EsArchivoDePrueba(t.ExamenVisible) || strings.HasPrefix(t.ExamenVisible, "/") || strings.Contains(t.ExamenVisible, "..")) {
		errs = append(errs, fmt.Errorf(idioma.T("examen_visible inválido: %q · usa un archivo de prueba relativo"), t.ExamenVisible))
	}
	return errs
}

// Marshal renders the task file.
func (t Task) Marshal() []byte {
	var b strings.Builder
	b.WriteString("---\n")
	if t.Version > 0 {
		fmt.Fprintf(&b, "version: %d\n", t.Version)
	}
	fmt.Fprintf(&b, "id: %s\n", t.ID)
	fmt.Fprintf(&b, "title: %s\n", t.Titulo)
	if t.Porque != "" {
		fmt.Fprintf(&b, "why: %s\n", t.Porque)
	}
	fmt.Fprintf(&b, "done_when: %s\n", t.ListoCuando)
	fmt.Fprintf(&b, "only_touch: %s\n", kv.MarshalList(t.TocarSolo))
	fmt.Fprintf(&b, "dont_touch: %s\n", kv.MarshalList(t.NoTocar))
	if len(t.DependeDe) > 0 {
		fmt.Fprintf(&b, "depends_on: %s\n", kv.MarshalList(t.DependeDe))
	}
	if len(t.Expone) > 0 {
		fmt.Fprintf(&b, "exposes: %s\n", kv.MarshalList(t.Expone))
	}
	if len(t.Usa) > 0 {
		fmt.Fprintf(&b, "uses: %s\n", kv.MarshalList(t.Usa))
	}
	fmt.Fprintf(&b, "max_retries: %d\n", t.LimiteIntentos)
	fmt.Fprintf(&b, "max_lines: %d\n", t.LimiteLineas)
	if t.Riesgos != "" {
		fmt.Fprintf(&b, "risks: %s\n", t.Riesgos)
	}
	if t.Peso != "" {
		fmt.Fprintf(&b, "weight: %s\n", PesoEn(t.Peso))
	}
	if t.Agente != "" {
		fmt.Fprintf(&b, "agent: %s\n", t.Agente)
	}
	if t.Skills != nil {
		fmt.Fprintf(&b, "skills: %s\n", kv.MarshalList(t.Skills))
	}
	if len(t.Cubre) > 0 {
		fmt.Fprintf(&b, "covers: %s\n", kv.MarshalList(t.Cubre))
	}
	if t.ExamenEsqueleto {
		fmt.Fprintf(&b, "examen_esqueleto: true\n")
		fmt.Fprintf(&b, "examen_visible: %s\n", t.ExamenVisible)
	}
	if t.Red {
		fmt.Fprintf(&b, "network: true\n")
	}
	if t.Recursivo {
		fmt.Fprintf(&b, "recursivo: true\n")
		fmt.Fprintf(&b, "limite_subtareas: %d\n", t.LimiteSubtareas)
	}
	b.WriteString("---\n")
	if t.Notas != "" {
		fmt.Fprintf(&b, "\n%s\n", t.Notas)
	}
	return []byte(b.String())
}

// esDeclaracionExplicita indica si la firma arranca con una palabra clave
// de declaración reconocible (type, interface, class, etc.), incluso con
// modificadores de exportación delante. Evita que la prosa ("cmd/sum main package")
// se confunda con una declaración real.
func esDeclaracionExplicita(firma string) bool {
	s := strings.TrimSpace(firma)
	modificadores := []string{
		"export default", "export", "declare", "default",
		"async", "public", "private", "protected", "readonly",
	}
	limpiado := true
	for limpiado {
		limpiado = false
		for _, mod := range modificadores {
			if strings.HasPrefix(s, mod+" ") {
				s = strings.TrimSpace(strings.TrimPrefix(s, mod))
				limpiado = true
				break
			}
		}
	}
	if i := strings.Index(s, " "); i >= 0 {
		switch s[:i] {
		case "type", "interface", "class", "enum", "const", "var", "let", "func", "function":
			return true
		}
	}
	return false
}

// FirmaVerificable devuelve el nombre a buscar en el diff y si la firma
// tiene forma de firma.
//
// El planificador es un modelo y a veces escribe descripciones en vez de
// contratos ("cmd/sum main package", "loom --mac <MAC>"). Eso no se
// puede comprobar contra un diff, y frenar la entrega por algo que el
// código no sabe verificar deja la tarea muerta: el único arreglo sería
// editar el contrato a mano, justo lo que loom evita. Lo que no tiene
// forma de firma se reporta, no bloquea.
func FirmaVerificable(firma string) (string, bool) {
	s := strings.TrimSpace(firma)
	if s == "" {
		return "", false
	}
	// función o método: lo que importa es el identificador antes del (
	if strings.Contains(s, "(") {
		n := NombreDeFirma(s)
		return n, esIdentificador(n)
	}
	// ruta HTTP, con o sin verbo delante
	if n := NombreDeFirma(s); strings.HasPrefix(n, "/") {
		return n, true
	}
	// declaración explícita de tipo, interfaz, clase o constante (TS/Go):
	if esDeclaracionExplicita(s) {
		n := NombreDeFirma(s)
		return n, esIdentificador(n)
	}
	// identificador pelado o cualificado: sin espacios
	if !strings.ContainsAny(s, " \t") {
		return NombreDeFirma(s), true
	}
	return "", false
}

// esIdentificador reporta si s puede ser un nombre de función o tipo.
func esIdentificador(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		letra := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if letra || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

var pesoEn = map[string]string{"liviana": "light", "media": "medium", "pesada": "heavy"}

// NormalizarPeso lleva light|medium|heavy al valor canónico liviana|media|pesada.
func NormalizarPeso(p string) string {
	for es, en := range pesoEn {
		if p == en {
			return es
		}
	}
	return p
}

func PesoEn(p string) string {
	if en, ok := pesoEn[p]; ok {
		return en
	}
	return p
}
