// Package esqueleto es la fase en que el modelo grande deja el plano del
// proyecto en código: estructura, ARCHITECTURE.md, tipos e interfaces
// declarados y un stub por clase o función con su firma y su contrato
// (a quién llama, casos de ejemplo). No escribe lógica ni pruebas: eso
// lo hacen los agentes baratos, cada uno su archivo y su prueba.
//
// Reemplaza al plan en prosa: las firmas que antes viajaban como texto
// en expone/usa y había que comparar a mano (firmaCanonica, examinador
// ciego, nivel semántico) ahora las valida el compilador.
//
// En una corrida real, el arquitecto escribió además 4.3k líneas de pruebas, 1.2k
// de cableado real e implementaciones de referencia: 292k tokens y
// $6.90, el 70% de la corrida. El plano cuesta una fracción de eso.
package esqueleto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/Pastranauwu/loom/internal/capturas"
	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/plan"
	"github.com/Pastranauwu/loom/internal/task"
)

// Marca es el texto con que lanza todo stub. La verificación no depende
// de él, pero le dice al agente que rellena qué reemplazar.
const Marca = "loom: sin implementar"

// Documento es el archivo de arquitectura que el esqueleto deja en la
// raíz y que cada cambio siguiente evoluciona.
const Documento = "ARCHITECTURE.md"

// Resultado es lo que devuelve el arquitecto además de lo que escribió
// en disco.
type Resultado struct {
	// Verificar compila o hace typecheck del proyecto entero y hoy pasa.
	Verificar string `json:"verificar"`
	// Integracion corre la prueba de punta a punta; hoy falla por los
	// stubs y pasa cuando todo está rellenado. Es la aceptación global.
	Integracion string `json:"integracion"`
	// Pruebas corre la suite completa del proyecto. En un monorepo
	// loom no la detecta, y la esclusa de salida la necesita.
	Pruebas      string `json:"pruebas"`
	Arquitectura string `json:"arquitectura"`
	// Pantallas dice cómo levantar la interfaz web y qué capturar: lo
	// usan el revisor visual y la entrega. Vacío si no hay web.
	Pantallas capturas.Pantallas `json:"pantallas"`
	Tareas    []plan.Borrador
}

// Pedido es lo que el humano quiere, ya redactado.
type Pedido struct {
	Texto string // feature, requirements, reglas y aceptación
	// Previo es el ARCHITECTURE.md que ya existe: el esqueleto lo
	// evoluciona en vez de reescribir el proyecto.
	Previo string
	// PrimerID es el id de la primera tarea de relleno.
	PrimerID string
	// SinDocker apaga la exigencia de compose: el humano lo pidió en sus
	// reglas (una librería o un CLI no se despliegan).
	SinDocker bool
	// Capturas son fotos de cómo se ve hoy la interfaz, en tamaño
	// celular. Un plan visual hecho sin verlas planea contra el código,
	// no contra lo que ve el usuario.
	Capturas []string
	// SuiteRota es la cola de la suite del proyecto cuando ya falla en la
	// base, antes de cualquier cambio; "" si pasa o no se pudo saber.
	SuiteRota string
}

// Prompt arma la instrucción del arquitecto. Lo fijo va primero para que
// una corrección (PromptCorregir) comparta el prefijo en caché.
func Prompt(p Pedido, c plan.Contexto) string {
	var b strings.Builder
	b.WriteString("Eres el ARQUITECTO de loom. No implementas la lógica: dejas el proyecto listo para que muchos agentes baratos lo rellenen en paralelo, cada uno un archivo, sin verse entre sí. Lo que ellos no pueden decidir lo decides tú ahora, en código.\n\n")
	b.WriteString("PEDIDO\n")
	b.WriteString(p.Texto)
	b.WriteString("\n")
	if c.Constitucion != "" {
		b.WriteString("\nCONSTITUCIÓN DEL PROYECTO (respétala)\n" + c.Constitucion + "\n")
	}
	b.WriteString("\nREPOSITORIO\n")
	b.WriteString(contexto(c))
	if strings.TrimSpace(p.Previo) != "" {
		b.WriteString("\nYa existe " + Documento + ". Evoluciónalo: agrega lo nuevo, ajusta lo que cambia y no reescribas lo que ya funciona. Las tareas son solo para lo nuevo o lo que cambia.\n")
	}
	if len(p.Capturas) > 0 {
		b.WriteString("\nASÍ SE VE HOY (capturas en tamaño celular tomadas hace un momento, de este mismo repositorio con el código actual: no están desactualizadas. Ábrelas con tu herramienta para leer archivos antes de planear)\n")
		for _, c := range p.Capturas {
			b.WriteString("- " + c + "\n")
		}
		b.WriteString("Si el pedido toca cómo se ve, tu plan tiene que cambiar lo que ves ahí de forma que se note.\n")
	}
	b.WriteString(`
SI EL REPOSITORIO YA TIENE CÓDIGO
- Las capturas mandan sobre el código: si el código dice que algo existe pero en la captura no se ve, o se ve roto o sin estilo, está mal y hay que planearlo.
- Si de verdad no hay nada que cambiar, responde con "tareas": [] y no inventes trabajo.
- Lo que ya funciona NO se convierte en stub. Respeta el stack, las librerías, el sistema de diseño y las convenciones que ya hay; no migres nada que el pedido no pida.
- Un cambio a código existente es una tarea sin stub: en "como" va qué cambiar, dónde y "Casos:" del comportamiento nuevo (entrada → salida o lo que se ve en pantalla). Si el cambio agrega funciones, métodos o componentes nuevos, esos sí van como stub con su contrato.
- El "listo_cuando" de un cambio corre una prueba NUEVA del comportamiento nuevo (un archivo que hoy no existe): las pruebas que ya existen pasan hoy y no sirven de oráculo. Esas pruebas tienen que seguir pasando.
- Una prueba que ya existe está congelada: el agente no puede tocarla. Si el cambio que pides vuelve FALSO algo que una prueba existente verifica (cambia un texto, un flujo, un resultado), eso lo decides tú, no el agente: pon ese archivo de prueba en el "tocar_solo" de la tarea, con su ruta exacta (un glob no lo descongela), y escribe en su "como" una línea "Obsoleto:" con qué casos de esa prueba dejan de valer y qué deben verificar ahora. El agente solo puede cambiar eso. Si ninguna prueba existente queda falsa, no pongas pruebas existentes en "tocar_solo".
- Si el pedido es visual (rediseño, estilo, experiencia de uso), el resultado tiene que VERSE distinto, no solo cambiar clases. Las pruebas existentes que fijan clases CSS o estilos no protegen nada del usuario: reescríbelas o bórralas tú en este esqueleto para que no frenen el rediseño. No hagas cambios "aditivos" para esquivarlas.
- Las pruebas de interfaz que pidas verifican lo que el usuario ve y hace (textos, roles, estados, navegación), no clases CSS: cómo se ve lo juzga una revisión con capturas.
`)
	b.WriteString(`
TU ENTREGA ES UN PLANO EN CÓDIGO, NO UNA IMPLEMENTACIÓN
No escribes lógica ni pruebas: cada línea que escribas la paga el modelo caro, y esa es la parte que hacen los agentes baratos. Tu trabajo es que encajen sin verse: dónde va cada cosa, qué clases y métodos existen, qué recibe y devuelve cada uno, quién llama a quién.

1. Estructura y stack: carpetas, manifiestos (package.json, pyproject.toml, go.mod, ...), configuración de build y del runner de pruebas, y .gitignore. Instala las dependencias.
   Interfaz web: usa el sistema de diseño y los componentes que el proyecto ya tenga. Solo si no hay ninguno y las reglas no piden otra cosa: Tailwind CSS y la librería de componentes estándar de su framework (p. ej. shadcn/ui en React), configuradas con su CLI sin preguntas (--yes/--defaults), y los componentes base que la app va a usar agregados con ese CLI: son código generado, no los escribas tú. El tema (colores, radios, tipografía) vive en un solo lugar.
2. ` + Documento + ` en la raíz: estilo (modular, hexagonal o en capas según el problema, no por moda), cada módulo con su archivo y su responsabilidad, qué módulo puede depender de cuál, flujo de datos del caso principal, y cómo se prueba cada módulo. Si hay interfaz web, una sección de UI: el sistema de diseño (dónde viven el tema y los componentes), la plantilla de página (layout, encabezado, estados cargando/vacío/error) y qué componente se usa para qué. Es la fuente de verdad que leen todos los agentes y que el próximo cambio evoluciona.
3. Tipos compartidos, entidades, interfaces y puertos: solo DECLARACIONES (campos, firmas de métodos). Nada de cuerpos con lógica.
4. Todo lo NUEVO es STUB, también el cableado nuevo (main, rutas, contenedor, App): cada clase, función y método público con su firma exacta (tipos de entrada y salida) y un comentario de contrato encima con
   - qué hace, entradas, salida y errores;
   - a quién llama (módulo y método) y quién lo usa;
   - "Idea:" cómo se resuelve, ya pensado por ti: algoritmo, estructuras de datos, orden de los pasos y las trampas (casos límite, lo que parece obvio y está mal). Telegráfico, de 1 a 8 líneas, sin código ni frases completas: tú ya resolviste el problema al planear y el agente solo lo escribe. No repitas lo que la firma ya dice.
   - "Casos:" de 2 a 5 ejemplos concretos de entrada → salida o error, que el agente convertirá en su prueba.
   Las etiquetas "Idea:" y "Casos:" son LITERALES: la palabra y los dos puntos pegados, sin nada en medio, también en el "como" de un cambio a código existente. "Casos (con mocks):" o "Ejemplos:" no cuentan: loom las busca por texto exacto y te devuelve el plano.
   El cuerpo solo lanza un error con el texto exacto "` + Marca + `".
5. Nada más. No escribas pruebas, implementaciones de referencia, datos de ejemplo ni código fuera del repositorio. No corras nada salvo lo necesario para que "verificar" pase.
`)
	if !p.SinDocker {
		b.WriteString(`6. Despliegue con Docker (es configuración: la escribes tú, completa). Un Dockerfile por servicio (multi-etapa, imagen final sin herramientas de desarrollo), un compose.yaml en la raíz que levanta todo con "docker compose up --build" (servicios, base de datos si hay, volúmenes para los datos, puertos, variables desde .env) y un .env.example, y un .dockerignore que deje fuera dependencias, builds locales, .git y .env. El README empieza por ese comando. Si ya existen, evoluciónalos. "docker compose config" tiene que pasar.
`)
	}
	b.WriteString(`

CÓMO REPARTIR
- Una tarea por módulo: un archivo stub, o varios que comparten estado o no tienen sentido por separado, o un cambio a un archivo existente. Con la "Idea:" escrita, un agente barato puede con un módulo complejo entero: no partas en tareas mínimas lo que se entiende junto, ni subas el "peso" de una tarea porque sea difícil de pensar; eso ya lo pensaste tú. El peso sube solo por tamaño o por interfaz.
- Excepción: un pedido VISUAL (diseño, estilo, experiencia de uso) no se reparte en tareas chicas. Es UNA tarea de diseño con "peso": "pesada", dueña de todos los archivos de interfaz que cambian (tema, componentes y pantallas en su "tocar_solo"), porque un diseño coherente no sale de parches de agentes que no ven la pantalla completa. En "como" describe el diseño concreto: paleta, tipografía, espaciado, cómo se ve cada pantalla y cada estado. Las tareas funcionales que toquen esos archivos dependen de ella. Cuanto más chicas e independientes, más agentes en paralelo y más barato.
- Cada tarea con stub declara en "listo_cuando" un archivo de prueba nuevo y lo lista en "tocar_solo" junto al stub. loom lo retira del alcance del implementador y un examinador independiente escribe esa prueba desde las firmas y "Casos:". Los cambios sin stub conservan la escritura de su prueba por el implementador.
- El agente es un modelo barato: no rediseña ni elige librerías. Lo que necesite decidir tiene que estar en el contrato.
- Las dependencias entre módulos entran por parámetro o constructor (puertos), para que cada módulo se pruebe con fakes sin esperar a los demás. "depende_de" solo cuando un módulo necesita de verdad la implementación real de otro.
- Una tarea final escribe la prueba de punta a punta del flujo principal: depende de todas y su "listo_cuando" es el comando de "integracion". Si ya existe una prueba de punta a punta, esa pasa hoy: si lo que cambia es comportamiento, la tarea final escribe una NUEVA y "integracion" corre la nueva; si el cambio no altera el flujo (estilos, textos), no hay tarea final y "integracion" es la prueba que ya existe, como regresión.

ANTES DE RESPONDER: el comando de "verificar" pasa con los stubs (compila, typecheck o importa todo). Nada más.

RESPONDE AL FINAL SOLO CON ESTE JSON
{
  "verificar": "comando que compila o hace typecheck de todo el proyecto y hoy pasa (ej. \"npx tsc --noEmit\", \"go vet ./...\", \"python -m compileall -q src\")",
  "integracion": "comando que corre la prueba de punta a punta (la escribe la tarea final)",
  "pruebas": "comando que corre TODA la suite del proyecto (todas las carpetas y lenguajes)",
  "arquitectura": "resumen de 5 a 15 líneas; lo completo vive en ` + Documento + `",
  "pantallas": {"levantar": "comando que sirve la app completa en el puerto $PORT, con build si hace falta (omite todo el campo si no hay interfaz web)", "url": "http://localhost:$PORT", "rutas": ["/", "cada pantalla principal, con su ruta tal como se abre en el navegador"], "semilla": "comando que carga datos de ejemplo con la app levantada en $PORT, para que las capturas no muestren solo estados vacíos (opcional)", "script": "comando que recorre con un navegador los flujos que no tienen ruta (elegir una foto, llenar un formulario, generar un resultado) y guarda un PNG por paso en $CAPTURAS; recibe $BASE_URL. Escríbelo tú (p. ej. Playwright): es OBLIGATORIO si alguna tarea cambia la interfaz"},
  "tareas": [
    {
      "titulo": "frase corta en minúscula",
      "porque": "qué requerimiento cubre",
      "cubre": ["los ids de requerimiento que implementa, tal como vienen en el pedido: \"R-a1b2c3\""],
      "listo_cuando": "comando que corre solo la prueba de este módulo",
      "tocar_solo": ["el stub o el archivo que cambia", "su archivo de prueba"],
      "depende_de": [],
      "peso": "liviana | media | pesada",
      "como": "qué archivo rellenar o qué cambiar; si es un cambio a código existente, su Idea: y sus Casos:"
    }
  ]
}
`)
	if p.SuiteRota != "" {
		b.WriteString("\nESTADO DE LA BASE\nLa suite del proyecto ya falla hoy, antes de tu cambio. Así termina su salida:\n" + p.SuiteRota + "\nSi ese fallo impide que corran las pruebas de tus tareas (la base de datos de pruebas no se construye, un import roto, una dependencia que falta), arréglalo tú en el esqueleto: es estado de la base, no lógica nueva, y sin eso ninguna tarea puede salir verde. Si no las afecta, no lo toques y no lo uses en ningún \"listo_cuando\".\n")
	}
	if p.PrimerID != "" {
		fmt.Fprintf(&b, "Tus tareas reciben ids correlativos desde %s en el orden del array: en \"depende_de\" escribe esos ids completos (\"%s\"), no números sueltos.\n", p.PrimerID, p.PrimerID)
	}
	return b.String()
}

// PromptCorregir repite el pedido original (mismo prefijo, mismo caché)
// y agrega lo que la verificación encontró. El repo conserva el trabajo
// anterior: se corrige, no se empieza de nuevo.
func PromptCorregir(original string, problemas []string) string {
	var b strings.Builder
	b.WriteString(original)
	b.WriteString("\nYA TRABAJASTE SOBRE ESTE REPOSITORIO. Lo que escribiste sigue en disco, pero la verificación de loom encontró esto:\n")
	for _, p := range problemas {
		b.WriteString("- " + p + "\n")
	}
	b.WriteString("Corrígelo en el repositorio y responde otra vez con el JSON completo.\n")
	return b.String()
}

// PromptCorregirSesion es la corrección para un arquitecto que continúa
// su propia conversación: ya tiene el pedido y lo que leyó del repo, así
// que solo recibe lo que la verificación encontró.
func PromptCorregirSesion(problemas []string) string {
	var b strings.Builder
	b.WriteString("La verificación de loom encontró esto en lo que dejaste:\n")
	for _, p := range problemas {
		b.WriteString("- " + p + "\n")
	}
	b.WriteString("Corrígelo en el repositorio sin volver a explorar lo que ya conoces y responde otra vez con el JSON completo.\n")
	return b.String()
}

func contexto(c plan.Contexto) string {
	switch {
	case c.EsVacio && c.Stack != "":
		return "Vacío. El humano eligió el stack: " + c.Stack + ".\n"
	case c.EsVacio:
		return "Vacío. Elige el stack que mejor sirva al pedido y respeta las reglas.\n"
	case c.Lenguaje == "":
		return "Ya tiene código. Inspecciónalo antes de decidir: respeta su stack, su estructura y sus corredores de pruebas.\n"
	default:
		s := "Ya tiene código en " + c.Lenguaje + ". Respeta su stack y su estructura."
		if c.Pruebas != "" {
			s += " Pruebas del proyecto: " + c.Pruebas + "."
		}
		return s + "\n"
	}
}

// Parse lee el JSON final del arquitecto.
func Parse(texto string) (Resultado, error) {
	bs, err := plan.Parse(texto)
	// sin tareas es una respuesta válida del arquitecto: lo pedido ya
	// está. Exigirle tareas lo hizo copiar el plan anterior
	if err != nil && !errors.Is(err, plan.ErrSinTareas) {
		return Resultado{}, err
	}
	t := plan.CuerpoJSON(texto)
	var r Resultado
	if !strings.HasPrefix(t, "{") {
		return Resultado{}, errors.New("el arquitecto no devolvió un objeto JSON")
	}
	if err := json.NewDecoder(strings.NewReader(plan.EscaparControles(t))).Decode(&r); err != nil {
		return Resultado{}, fmt.Errorf("el arquitecto devolvió JSON inválido · %s", err)
	}
	r.Tareas = bs
	return r, nil
}

// Verificacion es dónde y cómo revisar lo que dejó el arquitecto.
type Verificacion struct {
	Dir string
	// Base es el commit con que arrancó el cuarto. Lo que ya existía ahí
	// es código que funciona: se cambia, no se convierte en stub. Vacío
	// = todo es nuevo.
	Base    string
	Docker  bool // exigir compose.yaml y .dockerignore
	Timeout time.Duration
	Env     []string
}

// Problemas verifica el plano sin ningún modelo. Vacío es listo para
// repartir. No exige pruebas: las escribe cada tarea a partir de los
// casos de su contrato.
func Problemas(ctx context.Context, v Verificacion, r Resultado) []string {
	dir := v.Dir
	var out []string
	if _, err := os.Stat(filepath.Join(dir, Documento)); err != nil {
		out = append(out, "falta "+Documento+" en la raíz")
	}
	if strings.TrimSpace(r.Verificar) == "" {
		out = append(out, "falta \"verificar\": el comando que compila o hace typecheck del proyecto")
	} else if salida, code := correr(ctx, dir, r.Verificar, v.Timeout, v.Env); code == nil || *code != 0 {
		out = append(out, fmt.Sprintf("\"verificar\" (%s) no pasa con los stubs: %s", r.Verificar, cola(salida)))
	}
	if v.Docker {
		out = append(out, docker(ctx, dir, v.Timeout, v.Env)...)
	}
	archivos := versionables(ctx, dir)
	previos := enCommit(ctx, dir, v.Base)
	integracion := false
	for i, t := range r.Tareas {
		nombre := fmt.Sprintf("tarea %d (%s)", i+1, t.Titulo)
		if strings.TrimSpace(t.ListoCuando) == "" {
			out = append(out, nombre+": sin listo_cuando")
		}
		if strings.TrimSpace(t.ListoCuando) == strings.TrimSpace(r.Integracion) {
			integracion = true
		}
		if len(t.TocarSolo) == 0 {
			out = append(out, nombre+": tocar_solo vacío")
			continue
		}
		for _, f := range t.TocarSolo {
			if !strings.ContainsAny(f, "*?[") && !task.EsArchivoDePrueba(f) && !algunoCoincide([]string{f}, archivos) {
				out = append(out, fmt.Sprintf("%s: %s no existe; el stub lo creas tú", nombre, f))
			}
		}
		out = append(out, contrato(dir, nombre, t, archivos, previos)...)
	}
	// "integracion" es la aceptación global. Si falla hoy (proyecto nuevo,
	// o una prueba nueva que todavía no existe), una tarea final la hace
	// pasar. Si ya pasa, es la regresión de un proyecto existente: vale
	// como aceptación pero ninguna tarea puede usarla como listo_cuando,
	// porque la esclusa rechaza lo que ya pasa (la tarea final de
	// la UI renovada murió así). Un cambio solo visual no necesita una
	// prueba de punta a punta nueva.
	if len(r.Tareas) > 0 && strings.TrimSpace(r.Integracion) != "" {
		_, code := correr(ctx, dir, r.Integracion, v.Timeout, v.Env)
		pasa := code != nil && *code == 0
		switch {
		case pasa && integracion:
			out = append(out, fmt.Sprintf("\"integracion\" (%s) ya pasa hoy: una tarea con ese listo_cuando no prueba nada. Si lo nuevo necesita prueba de punta a punta, que la tarea final escriba una nueva y \"integracion\" la corra; si no, quita esa tarea y deja \"integracion\" como regresión", r.Integracion))
		case !pasa && !integracion:
			out = append(out, "ninguna tarea tiene como listo_cuando el comando de \"integracion\", que hoy falla: falta la tarea final que escribe la prueba de punta a punta")
		}
	}
	return out
}

// contrato exige que cada tarea diga qué hacer y cómo probarlo. En un
// archivo nuevo, un stub con la marca (prueba que el arquitecto no
// escribió la lógica) y "Casos:" en su comentario. En un archivo que ya
// existía, el código funciona y no se convierte en stub: el contrato del
// cambio va en "como", con sus casos. Antes se exigía la marca siempre y
// ningún cambio a código existente pasaba la verificación. La tarea
// final de integración solo escribe pruebas y no necesita nada.
func contrato(dir, nombre string, t plan.Borrador, archivos []string, previos map[string]bool) []string {
	var nuevos, existentes, pruebasViejas []string
	for _, f := range archivos {
		if previos[f] && task.EsArchivoDePrueba(f) && slices.Contains(t.TocarSolo, f) {
			pruebasViejas = append(pruebasViejas, f)
		}
		if !config.MatchesAny(t.TocarSolo, f) || task.EsArchivoDePrueba(f) {
			continue
		}
		if previos[f] {
			existentes = append(existentes, f)
		} else {
			nuevos = append(nuevos, f)
		}
	}
	var out []string
	if len(nuevos) > 0 {
		marca, casos, idea := false, false, false
		for _, f := range nuevos {
			b, err := os.ReadFile(filepath.Join(dir, f))
			if err != nil {
				continue
			}
			marca = marca || strings.Contains(string(b), Marca)
			casos = casos || strings.Contains(string(b), "Casos:")
			idea = idea || strings.Contains(string(b), "Idea:")
		}
		if !marca {
			out = append(out, fmt.Sprintf("%s: %s es nuevo y no tiene ningún stub con \"%s\"; lo nuevo lo implementa el agente, no tú", nombre, strings.Join(nuevos, ", "), Marca))
		}
		if !casos {
			out = append(out, fmt.Sprintf("%s: el contrato de %s no trae \"Casos:\"; sin ejemplos el agente no sabe qué probar", nombre, strings.Join(nuevos, ", ")))
		}
		// lo pensado por el modelo caro viaja en el contrato: sin eso el
		// barato rediseña, y solo se le podían dar tareas triviales
		if !idea {
			out = append(out, fmt.Sprintf("%s: el contrato de %s no trae \"Idea:\"; escribe en pocas líneas cómo se resuelve (algoritmo, estructuras, trampas) para que el agente solo lo implemente", nombre, strings.Join(nuevos, ", ")))
		}
	}
	// una prueba existente en tocar_solo deja de estar congelada: sin
	// decir qué parte quedó obsoleta, el agente podría reescribirla entera
	if len(pruebasViejas) > 0 && !strings.Contains(t.Como, "Obsoleto:") {
		out = append(out, fmt.Sprintf("%s: deja modificar %s, una prueba que ya existe, y su \"como\" no trae \"Obsoleto:\" con los casos que dejan de valer; si ninguno queda falso, sácala de tocar_solo", nombre, strings.Join(pruebasViejas, ", ")))
	}
	if len(nuevos) > 0 {
		return out
	}
	if len(existentes) > 0 && !strings.Contains(t.Como, "Casos:") {
		out = append(out, fmt.Sprintf("%s: cambia %s, que ya existe, y su \"como\" no trae \"Casos:\" del comportamiento nuevo", nombre, strings.Join(existentes, ", ")))
	}
	return out
}

// Composes son los nombres que acepta docker compose.
var Composes = []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"}

// docker exige lo necesario para levantar el proyecto con un comando:
// compose y .dockerignore (sin él, node_modules y .venv viajan al
// contexto de build). Si docker está instalado, además valida el compose.
func docker(ctx context.Context, dir string, timeout time.Duration, env []string) []string {
	compose := ""
	for _, n := range Composes {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			compose = n
			break
		}
	}
	var out []string
	if compose == "" {
		out = append(out, "falta compose.yaml en la raíz: el proyecto se levanta con \"docker compose up --build\"")
	}
	if _, err := os.Stat(filepath.Join(dir, ".dockerignore")); err != nil {
		out = append(out, "falta .dockerignore: sin él las dependencias y builds locales entran al contexto de docker")
	}
	if compose != "" {
		if _, err := exec.LookPath("docker"); err == nil {
			if salida, code := correr(ctx, dir, "docker compose config -q", timeout, env); code == nil || *code != 0 {
				out = append(out, "\"docker compose config\" no pasa: "+cola(salida))
			}
		}
	}
	return out
}

// enCommit lista los archivos versionados en base; nada si base es vacío.
func enCommit(ctx context.Context, dir, base string) map[string]bool {
	out := map[string]bool{}
	if base == "" {
		return out
	}
	cmd := exec.CommandContext(ctx, "git", "ls-tree", "-r", "--name-only", base)
	cmd.Dir = dir
	b, _ := cmd.Output()
	for _, f := range strings.Fields(string(b)) {
		out[f] = true
	}
	return out
}

// ConStub reporta si alguno de los archivos de tocar (no de prueba)
// tiene la marca: decide si la tarea rellena un stub o cambia código.
func ConStub(ctx context.Context, dir string, tocar []string) bool {
	for _, f := range versionables(ctx, dir) {
		if !config.MatchesAny(tocar, f) || task.EsArchivoDePrueba(f) {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(dir, f)); err == nil && strings.Contains(string(b), Marca) {
			return true
		}
	}
	return false
}

// SuiteBase corre la suite del proyecto tal como está y devuelve la cola
// de su salida si falla; "" si pasa, si no hay comando o si se pasó del
// tiempo (no concluyente). Es para saberlo antes de pagar un plan: con
// la suite rota en la base, las tareas que la necesitan no salen verdes
// hagan lo que hagan.
func SuiteBase(ctx context.Context, dir, comando string, timeout time.Duration, env []string) string {
	if strings.TrimSpace(comando) == "" {
		return ""
	}
	salida, code := correr(ctx, dir, comando, timeout, env)
	if code == nil || *code == 0 || *code == 124 {
		return ""
	}
	return cola(salida)
}

func correr(ctx context.Context, dir, cmdStr string, timeout time.Duration, env []string) (string, *int) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", task.SinTerminal(cmdStr))
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", task.SinTerminal(cmdStr))
	}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		c := 124
		return string(out) + "\nse pasó del tiempo", &c
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		c := exitErr.ExitCode()
		return string(out), &c
	}
	if err != nil {
		return string(out) + err.Error(), nil
	}
	c := 0
	return string(out), &c
}

// versionables lista los archivos del árbol que git vería, sin los
// ignorados ni los excluidos (node_modules y compañía).
func versionables(ctx context.Context, dir string) []string {
	cmd := exec.CommandContext(ctx, "git", "ls-files", "-co", "--exclude-standard")
	cmd.Dir = dir
	out, _ := cmd.Output()
	return strings.Fields(string(out))
}

func algunoCoincide(patrones, archivos []string) bool {
	for _, f := range archivos {
		if config.MatchesAny(patrones, f) {
			return true
		}
	}
	return false
}

// cola deja las últimas líneas de una salida para el mensaje.
func cola(s string) string {
	lineas := strings.Split(strings.TrimSpace(s), "\n")
	if len(lineas) > 6 {
		lineas = lineas[len(lineas)-6:]
	}
	return strings.Join(lineas, " ⏎ ")
}
