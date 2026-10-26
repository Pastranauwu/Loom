package plan

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/task"
)

type generadorFijo struct {
	texto string
	err   error
}

func (g generadorFijo) Generar(_ context.Context, _ string) (string, error) {
	return g.texto, g.err
}

func TestPromptPideJSON(t *testing.T) {
	p := Prompt("exportar clientes", Contexto{Lenguaje: "go", Pruebas: "go test ./..."})
	for _, want := range []string{"exportar clientes", "listo_cuando", "tocar_solo", "objeto JSON", "arquitectura"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt sin %q", want)
		}
	}
}

func TestPromptDivideYVenceras(t *testing.T) {
	p := Prompt("clonar repo", Contexto{Lenguaje: "go", Pruebas: "go test ./..."})
	for _, want := range []string{"Divide y vencerás", "ARQUITECTO", "no escribes código", "mismo nombre, mismos tipos"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt sin %q:\n%s", want, p)
		}
	}
	for _, viejo := range []string{"evita microtareas", "agrupa trabajo cohesivo"} {
		if strings.Contains(p, viejo) {
			t.Errorf("prompt todavía pide %q", viejo)
		}
	}
}

func TestPromptInyectaLenguaje(t *testing.T) {
	p := Prompt("exportar", Contexto{Lenguaje: "go", Pruebas: "go test ./..."})
	for _, want := range []string{"go", "go test ./..."} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt sin lenguaje/pruebas %q:\n%s", want, p)
		}
	}
}

func TestPromptRepoVacio(t *testing.T) {
	p := Prompt("crear wakeonlan", Contexto{EsVacio: true})
	for _, want := range []string{"vacío", "stack", "depende_de"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt greenfield sin %q:\n%s", want, p)
		}
	}
}

func TestPromptIncluyeRequisitosYStack(t *testing.T) {
	p := Prompt("crear wakeonlan", Contexto{EsVacio: true, Stack: "go", Requisitos: "servidor http local"})
	for _, want := range []string{"go", "servidor http local"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt sin %q:\n%s", want, p)
		}
	}
}

func TestParseArrayLimpio(t *testing.T) {
	bs, err := Parse(`[{"titulo":"exportar","listo_cuando":"go test ./..."}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 1 || bs[0].Titulo != "exportar" {
		t.Fatalf("bs = %+v", bs)
	}
}

func TestParseConVallasYTexto(t *testing.T) {
	texto := "Aquí tienes:\n```json\n[{\"titulo\":\"arreglar login\",\"listo_cuando\":\"npm test -- auth\",\"tocar_solo\":[\"src/auth/**\"]}]\n```\nEspero que sirva."
	bs, err := Parse(texto)
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 1 || bs[0].TocarSolo[0] != "src/auth/**" {
		t.Fatalf("bs = %+v", bs)
	}
}

func TestParseSinArray(t *testing.T) {
	if _, err := Parse("no hay array aquí"); err == nil {
		t.Fatal("texto sin array debió fallar")
	}
}

func TestParseBorradorIncompleto(t *testing.T) {
	if _, err := Parse(`[{"titulo":"sin listo"}]`); err == nil {
		t.Fatal("borrador sin listo_cuando debió fallar")
	}
}

func TestGenerar(t *testing.T) {
	g := generadorFijo{texto: `[{"titulo":"a","listo_cuando":"true"}]`}
	bs, err := Generar(context.Background(), g, Contexto{}, "haz a")
	if err != nil || len(bs) != 1 {
		t.Fatalf("Generar = %v, %v", bs, err)
	}
}

func TestPromptInyectaAgentes(t *testing.T) {
	ctx := Contexto{
		Lenguaje: "go",
		Agentes: map[string]config.Agente{
			"architect": {Provider: "claude", Skills: []string{"diseno", "arquitectura"}},
			"tester":    {Provider: "claude"},
		},
	}
	p := Prompt("crear módulo", ctx)
	for _, want := range []string{"architect (habilidades: diseno, arquitectura)", "tester", `"agente"`} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt sin %q:\n%s", want, p)
		}
	}
}

func TestParseBorradorConAgente(t *testing.T) {
	bs, err := Parse(`[{"titulo":"diseño","listo_cuando":"true","agente":"architect"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 1 || bs[0].Agente != "architect" {
		t.Fatalf("bs = %+v", bs)
	}
}

func TestAcotarLimiteLineas(t *testing.T) {
	for _, limite := range []int{0, 20, 200, 5000} {
		for _, propuesta := range []int{-50, 0, 3, 1200, 999999} {
			if got := AcotarLimiteLineas(propuesta, limite); got != limite {
				t.Fatalf("modelo=%d humano=%d: quedó %d", propuesta, limite, got)
			}
		}
	}
}

func TestParseLeeLimiteLineas(t *testing.T) {
	bs, err := Parse(`[{"titulo":"x","listo_cuando":"go test ./...","limite_lineas":1200}]`)
	if err != nil {
		t.Fatal(err)
	}
	if bs[0].LimiteLineas != 1200 {
		t.Errorf("limite_lineas = %d, quiero 1200", bs[0].LimiteLineas)
	}
}

// El prompt tiene que exigir que listo_cuando falle hoy. Sin eso el
// planificador copia el comando de pruebas del proyecto (`npm test`,
// `go test ./...`), que en un repo con la suite verde pasa de entrada, y
// la esclusa de entrada rechaza TODAS las tareas antes de gastar un token.
func TestPromptExigeQueListoCuandoFalleHoy(t *testing.T) {
	p := Prompt("lo que sea", Contexto{Lenguaje: "node", Pruebas: "npm test"})
	for _, quiero := range []string{"HOY FALLE", "rechaza la tarea si ya pasa", "NO copies ese comando tal cual"} {
		if !strings.Contains(p, quiero) {
			t.Errorf("el prompt no dice %q", quiero)
		}
	}
	// y da ejemplos acotados de cada stack, que es de donde el modelo copia
	for _, ej := range []string{"node --test test/", "pytest tests/", "go test ./internal/"} {
		if !strings.Contains(p, ej) {
			t.Errorf("el prompt no da el ejemplo %q", ej)
		}
	}
}

// En greenfield el aviso ya estaba; que no se pierda.
func TestPromptGreenfieldSigueExigiendoloTambien(t *testing.T) {
	p := Prompt("algo", Contexto{EsVacio: true})
	if !strings.Contains(p, "HOY FALLE") {
		t.Error("el requisito vale igual en un repo vacío")
	}
}

// Sin examinador ciego la tarea escribe sus propias pruebas, así que su
// archivo tiene que entrar en tocar_solo. Sin decírselo, el planificador
// apunta el listo_cuando a un archivo que deja fuera de alcance y que
// nadie puede crear: la tarea agota los intentos con "Could not find".
func TestPromptPideLasPruebasEnAlcanceSinExaminador(t *testing.T) {
	con := Prompt("algo", Contexto{Lenguaje: "node", PruebasPropias: true})
	if !strings.Contains(con, "ESE archivo tiene que estar también en \"tocar_solo\"") {
		t.Error("el prompt no pide meter el archivo de prueba en tocar_solo")
	}
	// con examinador la regla es la contraria y no debe aparecer
	sin := Prompt("algo", Contexto{Lenguaje: "go"})
	if strings.Contains(sin, "las pruebas las escribe la propia tarea") {
		t.Error("con examinador ciego el implementador NO escribe las pruebas")
	}
}

// Completar no reparte: un modelo que devuelve otra cantidad de contratos
// desalinearía cada tarea con el de su vecina.
func TestCompletarExigeUnoPorTarea(t *testing.T) {
	tareas := []task.Task{{ID: "T-003", Titulo: "a"}, {ID: "T-004", Titulo: "b"}}
	uno := `[{"titulo":"a","listo_cuando":"go test ./a"}]`
	if _, err := Completar(context.Background(), generadorFijo{texto: uno}, Contexto{}, "", nil, tareas); err == nil {
		t.Fatal("1 contrato para 2 tareas debió fallar")
	}
	dos := `[{"titulo":"a","listo_cuando":"go test ./a"},{"titulo":"b","listo_cuando":"go test ./b"}]`
	bs, err := Completar(context.Background(), generadorFijo{texto: dos}, Contexto{}, "", nil, tareas)
	if err != nil || len(bs) != 2 {
		t.Fatalf("Completar = %v, %v", bs, err)
	}
	p := PromptCompletar("wol", []string{"sin dependencias"}, tareas, Contexto{})
	for _, want := range []string{"exactamente 2", "1. T-003 · a", "2. T-004 · b", "sin dependencias", "\"T-003\""} {
		if !strings.Contains(p, want) {
			t.Errorf("el prompt no contiene %q", want)
		}
	}
}

// El prompt tiene que prohibir prometer de más: lo que una tarea soporta y
// su consumidora nunca pide no lo prueba nadie, aunque las dos queden
// verdes. Es la costura que dejaba `-2^2` roto con cinco tareas en verde.
func TestPromptProhibePrometerDeMas(t *testing.T) {
	p := Prompt("una calculadora", Contexto{Lenguaje: "go", Pruebas: "go test ./..."})
	for _, quiero := range []string{"no prometas de más", "nunca pide no lo prueba nadie", "tiene que cubrirlo"} {
		if !strings.Contains(p, quiero) {
			t.Errorf("el prompt no dice %q", quiero)
		}
	}
}

func TestParseArquitecturaCompartida(t *testing.T) {
	bs, err := Parse("```json\n" + `{"arquitectura":"Hexagonal: internal/domain/model.go define tipos; internal/http/router.go compone adaptadores.","tareas":[{"titulo":"dominio","listo_cuando":"go test ./internal/domain","como":"Validar entradas"},{"titulo":"http","listo_cuando":"go test ./internal/http","como":"Traducir errores"}]}` + "\n```")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bs {
		if !strings.Contains(b.Como, "internal/domain/model.go") || !strings.Contains(b.Como, "internal/http/router.go") {
			t.Fatalf("se perdió arquitectura: %+v", b)
		}
	}
	if !strings.Contains(bs[0].Como, "Validar entradas") || !strings.Contains(bs[1].Como, "Traducir errores") {
		t.Fatal("se perdieron instrucciones específicas")
	}
}

func TestParseObjetoSinArquitectura(t *testing.T) {
	if _, err := Parse(`{"tareas":[{"titulo":"x","listo_cuando":"false"}]}`); err == nil {
		t.Fatal("aceptó plan nuevo sin arquitectura")
	}
}

func TestEjemploDelPromptEsUnPlanValido(t *testing.T) {
	p := Prompt("feature", Contexto{})
	_, ejemplo, ok := strings.Cut(p, "Ejemplo:\n")
	if !ok {
		t.Fatal("falta ejemplo")
	}
	bs, err := Parse(ejemplo)
	if err != nil || len(bs) != 2 {
		t.Fatalf("ejemplo inválido: %v", err)
	}
}

// El planificador distingue "skills":[] (ninguna) de no decir nada (las
// del rol): perder esa diferencia al parsear le da frontend-design a una
// tarea que la pidió vacía.
func TestParseDistingueSkillsVaciasDeAusentes(t *testing.T) {
	bs, err := Parse(`[{"titulo":"hud","listo_cuando":"true","skills":[]},{"titulo":"menu","listo_cuando":"true","skills":["frontend-design"]},{"titulo":"x","listo_cuando":"true"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if bs[0].Skills == nil || len(bs[0].Skills) != 0 || len(bs[1].Skills) != 1 || bs[2].Skills != nil {
		t.Errorf("skills: %#v %#v %#v", bs[0].Skills, bs[1].Skills, bs[2].Skills)
	}
}

// respuesta real de deepseek-v4-flash: "arquitectura" con saltos de
// línea crudos dentro del string
func TestParseToleraSaltosCrudosEnStrings(t *testing.T) {
	b, err := os.ReadFile("testdata/plan-saltos-crudos.txt")
	if err != nil {
		t.Fatal(err)
	}
	bs, err := Parse(string(b))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(bs) == 0 || !strings.Contains(bs[0].Como, "ESTILO:") {
		t.Errorf("plan sin tareas o sin arquitectura: %d tareas", len(bs))
	}
}
