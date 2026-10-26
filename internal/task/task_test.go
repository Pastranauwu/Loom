package task

import (
	"reflect"
	"strings"
	"testing"
)

const contractEjemplo = `---
version: 1
id: T-001
titulo: exportar clientes a CSV
porque: soporte pierde 3h/semana copiando a mano
listo_cuando: npm test -- export.spec.ts     # OBLIGATORIO, ejecutable
tocar_solo: ["src/export/**"]
no_tocar: ["src/auth/**", "migrations/**"]
limite_intentos: 3
limite_lineas: 200
riesgos: archivos grandes pueden agotar memoria
---
Notas libres opcionales.
`

func TestParseContratoCompleto(t *testing.T) {
	task, err := Parse([]byte(contractEjemplo))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := Task{
		Version:        1,
		ID:             "T-001",
		Titulo:         "exportar clientes a CSV",
		Porque:         "soporte pierde 3h/semana copiando a mano",
		ListoCuando:    "npm test -- export.spec.ts",
		TocarSolo:      []string{"src/export/**"},
		NoTocar:        []string{"src/auth/**", "migrations/**"},
		LimiteIntentos: 3,
		LimiteLineas:   200,
		Riesgos:        "archivos grandes pueden agotar memoria",
		Notas:          "Notas libres opcionales.",
	}
	if !reflect.DeepEqual(task, want) {
		t.Errorf("Parse = %+v\nquiero %+v", task, want)
	}
}

func TestParseErrores(t *testing.T) {
	cases := map[string]string{
		"sin bloque inicial":    "id: T-001\n",
		"sin bloque de cierre":  "---\nid: T-001\n",
		"entero inválido":       "---\nversion: 1\nlimite_intentos: tres\n---\n",
		"lista mal formada":     "---\nversion: 1\ntocar_solo: src/**\n---\n",
		"elemento sin comillas": "---\nversion: 1\ntocar_solo: [src/**]\n---\n",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(input)); err == nil {
				t.Errorf("Parse debió fallar: %s", name)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	valida := Task{
		Version:        Version,
		ID:             "T-001",
		Titulo:         "algo",
		ListoCuando:    "make test",
		LimiteIntentos: 3,
		LimiteLineas:   200,
	}
	if errs := valida.Validate(); len(errs) != 0 {
		t.Errorf("contrato válido rechazado: %v", errs)
	}

	sinListo := valida
	sinListo.ListoCuando = ""
	errs := sinListo.Validate()
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "listo_cuando") {
		t.Errorf("Validate sin listo_cuando = %v", errs)
	}

	varios := Task{}
	if errs := varios.Validate(); len(errs) != 5 {
		t.Errorf("contrato vacío: %d errores, quiero 5: %v", len(errs), errs)
	}
}

func TestMarshalRoundtrip(t *testing.T) {
	original, err := Parse([]byte(contractEjemplo))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	reparsed, err := Parse(original.Marshal())
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if !reflect.DeepEqual(original, reparsed) {
		t.Errorf("roundtrip = %+v\nquiero %+v", reparsed, original)
	}
}

func TestMarshalOmiteVacios(t *testing.T) {
	task := Task{Version: Version, ID: "T-001", Titulo: "x", ListoCuando: "true", LimiteIntentos: 3, LimiteLineas: 200}
	out := string(task.Marshal())
	if strings.Contains(out, "porque") || strings.Contains(out, "riesgos") {
		t.Errorf("Marshal incluyó campos vacíos:\n%s", out)
	}
	if !strings.Contains(out, "tocar_solo: []") {
		t.Errorf("Marshal debió escribir listas vacías:\n%s", out)
	}
}

func TestParseSinVersion(t *testing.T) {
	task, err := Parse([]byte("---\nid: T-001\ntitulo: x\n---\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	errs := task.Validate()
	if len(errs) == 0 || !strings.Contains(errs[0].Error(), "falta version") {
		t.Errorf("Validate sin version = %v · quiere exigir version", errs)
	}
}

func TestParseVersionInvalida(t *testing.T) {
	for _, v := range []string{"cero", "0", "-1"} {
		if _, err := Parse([]byte("---\nversion: " + v + "\nid: T-001\n---\n")); err == nil {
			t.Errorf("Parse aceptó version: %s", v)
		}
	}
}

func TestParseVersionFuturaTolera(t *testing.T) {
	task, err := Parse([]byte("---\nversion: 2\nid: T-001\ntitulo: x\nlisto_cuando: make test\ncampo_del_futuro: algo\n---\n"))
	if err != nil {
		t.Fatalf("un contrato de versión futura debió parsear: %v", err)
	}
	if task.Aviso != "contrato versión 2, binario soporta 1 · actualiza loom" {
		t.Errorf("Aviso = %q", task.Aviso)
	}
	if task.Titulo != "x" {
		t.Errorf("los campos conocidos debieron leerse: %+v", task)
	}
}

func TestParseCampoDesconocidoEnVersionActual(t *testing.T) {
	task, err := Parse([]byte("---\nversion: 1\nid: T-001\ncampo_del_futuro: algo\n---\n"))
	if err != nil {
		t.Fatalf("Parse = %v · un campo desconocido debe tolerarse", err)
	}
	if !strings.Contains(task.Aviso, "campo desconocido ignorado") {
		t.Errorf("Aviso = %q · debió registrar advertencia de campo ignorado", task.Aviso)
	}
}

func TestParseDependeDe(t *testing.T) {
	task, err := Parse([]byte("---\nversion: 1\nid: T-002\ntitulo: x\nlisto_cuando: true\ndepende_de: [\"T-001\"]\n---\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(task.DependeDe) != 1 || task.DependeDe[0] != "T-001" {
		t.Errorf("DependeDe = %v, quiero [T-001]", task.DependeDe)
	}
	if !strings.Contains(string(task.Marshal()), "depende_de: [\"T-001\"]") {
		t.Errorf("Marshal no escribió depende_de:\n%s", task.Marshal())
	}
}

func TestValidateDependeDeInvalida(t *testing.T) {
	task := Task{Version: Version, ID: "T-001", Titulo: "x", ListoCuando: "true", LimiteIntentos: 3, LimiteLineas: 200, DependeDe: []string{"t-2"}}
	errs := task.Validate()
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "depende_de inválido") {
		t.Errorf("Validate depende_de mal formada = %v", errs)
	}

	ciclo := task
	ciclo.DependeDe = []string{"T-001"}
	if errs := ciclo.Validate(); len(errs) != 1 || !strings.Contains(errs[0].Error(), "sí misma") {
		t.Errorf("Validate dependencia propia = %v", errs)
	}
}

func TestPeso(t *testing.T) {
	task, err := Parse([]byte("---\nversion: 1\nid: T-001\ntitulo: x\nlisto_cuando: true\npeso: pesada\n---\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if task.Peso != "pesada" {
		t.Errorf("Peso = %q, quiero pesada", task.Peso)
	}
	if !strings.Contains(string(task.Marshal()), "peso: pesada") {
		t.Errorf("Marshal no escribió peso:\n%s", task.Marshal())
	}

	inv := Task{Version: Version, ID: "T-001", Titulo: "x", ListoCuando: "true", LimiteIntentos: 3, LimiteLineas: 200, Peso: "gigante"}
	if errs := inv.Validate(); len(errs) != 1 || !strings.Contains(errs[0].Error(), "peso inválido") {
		t.Errorf("Validate peso inválido = %v", errs)
	}
}

func TestNombreDeFirma(t *testing.T) {
	casos := map[string]string{
		"wol.Send(mac, addr string) error":          "Send",
		"POST /wake":                                "/wake",
		"GET /api/devices":                          "/api/devices",
		"func Cargar(path string) (*Config, error)": "Cargar",
		"Enviar": "Enviar",
		"":       "",
		"hue.NewBridge(addr string, l ...*Light) *Bridge": "NewBridge",
	}
	for firma, want := range casos {
		if got := NombreDeFirma(firma); got != want {
			t.Errorf("NombreDeFirma(%q) = %q, quiero %q", firma, got, want)
		}
	}
}

func TestExponeUsaIdaYVuelta(t *testing.T) {
	orig := Task{
		Version: Version, ID: "T-001", Titulo: "wol",
		ListoCuando: "go test ./...", LimiteIntentos: 3, LimiteLineas: 200,
		Expone: []string{"wol.Send(mac, addr string) error"},
		Usa:    []string{"config.Cargar(path string) (*Config, error)"},
	}
	got, err := Parse(orig.Marshal())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got.Expone) != 1 || got.Expone[0] != orig.Expone[0] {
		t.Errorf("Expone = %v", got.Expone)
	}
	if len(got.Usa) != 1 || got.Usa[0] != orig.Usa[0] {
		t.Errorf("Usa = %v", got.Usa)
	}
}

func TestAgenteIdaYVuelta(t *testing.T) {
	orig := Task{
		Version:        Version,
		ID:             "T-001",
		Titulo:         "diseñar esquema",
		ListoCuando:    "go test ./...",
		LimiteIntentos: 3,
		LimiteLineas:   200,
		Agente:         "architect",
	}
	raw := orig.Marshal()
	if !strings.Contains(string(raw), "agente: architect") {
		t.Fatalf("Marshal no incluyó agente:\n%s", raw)
	}
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Agente != "architect" {
		t.Errorf("Agente = %q, quiero architect", got.Agente)
	}

	inv := Task{Version: Version, ID: "T-001", Titulo: "x", ListoCuando: "true", LimiteIntentos: 3, LimiteLineas: 200, Agente: "con espacios"}
	if errs := inv.Validate(); len(errs) != 1 || !strings.Contains(errs[0].Error(), "agente inválido") {
		t.Errorf("Validate agente inválido = %v", errs)
	}
}

func TestFirmaVerificable(t *testing.T) {
	casos := []struct {
		firma  string
		nombre string
		ok     bool
	}{
		{"wol.Send(mac string) error", "Send", true},
		{"POST /wake", "/wake", true},
		{"/wake", "/wake", true},
		{"Ejecutar", "Ejecutar", true},
		// prosa: no hay nada que buscar en un diff
		{"cmd/sum main package", "", false},
		{"loom --mac <MAC>", "", false},
		{"", "", false},
	}
	for _, c := range casos {
		nombre, ok := FirmaVerificable(c.firma)
		if ok != c.ok || (ok && nombre != c.nombre) {
			t.Errorf("FirmaVerificable(%q) = (%q, %v), quiero (%q, %v)", c.firma, nombre, ok, c.nombre, c.ok)
		}
	}
}

// Una barra dentro de prosa no es una ruta HTTP.
func TestNombreDeFirmaNoConfundeProsaConRuta(t *testing.T) {
	if n := NombreDeFirma("cmd/sum main package"); strings.HasPrefix(n, "/") {
		t.Errorf("NombreDeFirma = %q · no es una ruta", n)
	}
	if n := NombreDeFirma("POST /wake"); n != "/wake" {
		t.Errorf("NombreDeFirma = %q, quiero /wake", n)
	}
}

// Los contratos que genera el planificador para stacks TS declaran tipos y
// constantes: "type Point = { x: number }" y "levels.LEVELS: Level[]". El
// nombre tiene que quedar pelado, igual que el de una función, o
// ValidatePlan marca la pieza como huérfana cuando la consumidora la usa.
func TestNombreDeFirmaTipoYConstanteTS(t *testing.T) {
	casos := map[string]string{
		"type Point = { x: number; y: number }":                      "Point",
		"export type Point = { x: number; y: number }":               "Point",
		"type SnakeState = { body: Point[]; direction: Direction }":  "SnakeState",
		"export interface ISynth<T> { play(): void }":                "ISynth",
		"interface SoundPlayer<T extends Node>":                      "SoundPlayer",
		"export class AudioManager {":                                "AudioManager",
		"class SynthVoice implements Voice":                          "SynthVoice",
		"export function noteToFreq(nota: string): number":           "noteToFreq",
		"export async function playSound(id: string): Promise<void>": "playSound",
		"const LEVELS: Level[]":                                      "LEVELS",
		"export const LEVELS: Level[]":                               "LEVELS",
		"levels.LEVELS: Level[]":                                     "LEVELS",
		"sound.SoundManager = { play(): void }":                      "SoundManager",
		"type Direction = 'up' | 'down' | 'left' | 'right'":          "Direction",
	}
	for firma, want := range casos {
		if got := NombreDeFirma(firma); got != want {
			t.Errorf("NombreDeFirma(%q) = %q, quiero %q", firma, got, want)
		}
	}
}

func TestFirmaVerificableTiposEInterfacesTS(t *testing.T) {
	verificables := map[string]string{
		"export interface ISynth<T> { play(): void }": "ISynth",
		"type Point = { x: number; y: number }":       "Point",
		"export class AudioManager":                   "AudioManager",
		"export const LEVELS: Level[]":                "LEVELS",
		"wol.Send(mac string) error":                  "Send",
		"POST /wake":                                  "/wake",
	}
	for firma, want := range verificables {
		got, ok := FirmaVerificable(firma)
		if !ok || got != want {
			t.Errorf("FirmaVerificable(%q) = (%q, %v), quiero (%q, true)", firma, got, ok, want)
		}
	}

	prosa := []string{"cmd/sum main package", "loom --mac <MAC>", "notas explicativas"}
	for _, p := range prosa {
		if got, ok := FirmaVerificable(p); ok {
			t.Errorf("FirmaVerificable(%q) = (%q, true) · no debió ser verificable", p, got)
		}
	}
}

func TestLimiteLineasOpcional(t *testing.T) {
	tk := Task{Version: Version, ID: "T-001", Titulo: "módulo", ListoCuando: "false", LimiteIntentos: 3}
	if errs := tk.Validate(); len(errs) != 0 {
		t.Fatal(errs)
	}
	tk.LimiteLineas = -1
	if errs := tk.Validate(); len(errs) != 1 {
		t.Fatalf("límite negativo: %v", errs)
	}
}

// skills distingue "no lo dice" (las del rol) de "ninguna": un contrato
// que declara skills: [] no debe recibir las del rol al releerse.
func TestSkillsIdaYVuelta(t *testing.T) {
	for _, quiero := range [][]string{nil, {}, {"frontend-design", "clean-code"}} {
		tk := Task{Version: Version, ID: "T-001", Titulo: "x", ListoCuando: "true", TocarSolo: []string{"a/**"}, Skills: quiero}
		got, err := Parse(tk.Marshal())
		if err != nil {
			t.Fatal(err)
		}
		if (got.Skills == nil) != (quiero == nil) || len(got.Skills) != len(quiero) {
			t.Errorf("skills %#v → %#v", quiero, got.Skills)
		}
	}
}

func TestCubreSobreviveAlGuardar(t *testing.T) {
	tk := Task{Version: Version, ID: "T-001", Titulo: "x", ListoCuando: "true", TocarSolo: []string{"a"}, Cubre: []string{"R-a1b2c3", "fondo"}, LimiteIntentos: 3}
	got, err := Parse([]byte(tk.Marshal()))
	if err != nil || len(got.Cubre) != 2 || got.Cubre[1] != "fondo" {
		t.Fatalf("%+v %v", got.Cubre, err)
	}
}

// una herramienta que pregunta (Django ante una base de pruebas que
// quedó de una corrida cortada) muere con EOFError si hay terminal
func TestSinTerminal(t *testing.T) {
	casos := map[string]string{
		"python manage.py test documentos.tests.test_x":               "python manage.py test --noinput documentos.tests.test_x",
		"compila && (python manage.py test a) && ./manage.py  test b": "compila && (python manage.py test --noinput a) && ./manage.py  test --noinput b",
		"python manage.py test --noinput a":                           "python manage.py test --noinput a",
		"python manage.py test a --no-input":                          "python manage.py test a --no-input",
		"go test ./...":                                               "go test ./...",
		"python manage.py testserver":                                 "python manage.py testserver",
	}
	for cmd, quiero := range casos {
		if got := SinTerminal(cmd); got != quiero {
			t.Errorf("SinTerminal(%q) = %q, quiero %q", cmd, got, quiero)
		}
	}
}
