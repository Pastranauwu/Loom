package ship

import (
	"context"
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Pastranauwu/loom/internal/capturas"
	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/spec"
	"github.com/Pastranauwu/loom/internal/task"
)

// RamaEntrega es la rama donde se juntan los commits de todas las tareas
// verdes antes de abrir un solo PR.
const RamaEntrega = "loom/_entrega"

// OpcionesEntrega lleva las dependencias de la entrega conjunta.
type OpcionesEntrega struct {
	Root    string
	Config  config.Config
	Base    string
	Tareas  []task.Task       // todas en estado lista
	Modelos map[string]string // id de tarea -> modelo del último intento
	// Commits lleva, por tarea, el commit desde el que se creó su cuarto.
	// Es lo que separa su trabajo del que heredó de una oleada anterior,
	// y a diferencia de los mensajes `wip:` sobrevive a que la esclusa
	// aplane la rama: sin él, una segunda pasada medía el cuarto entero.
	Commits map[string]string
	Titulo  string // título del PR; vacío usa el de la primera tarea
	Timeout time.Duration
	// Acceptance son comandos del spec humano y corren sobre el conjunto
	// integrado, después de la suite general y antes de crear el PR.
	Acceptance []string
	// Spec es el hash de la intención del spec que se entrega
	// (spec.Intencion.Hash); queda anotado en aceptacion.json.
	Spec string
	// Revisor, si no es nil, lee el diff completo antes de integrar y
	// puede vetar. Es el único paso que juzga intención en vez de
	// mecánica. Falla cerrado: lo que no se pudo revisar no se integra.
	Revisor Revisor
	// Integrar mergea el PR cuando el revisor no pide cambios. Sin él la
	// entrega termina con el PR abierto y el informe dentro: aprobar es
	// del humano.
	Integrar bool
	DryRun   bool
	Progreso func(Paso)
}

// Revisor juzga si la entrega puede integrarse. La interfaz vive aquí,
// del lado del consumidor, para no atar ship al paquete revisor.
type Revisor interface {
	// Revisar devuelve si el revisor no encontró nada que corregir, el
	// informe en markdown para el PR y una línea para la terminal.
	Revisar(ctx context.Context, diff string, tareas []task.Task) (sinCambios bool, informe, resumen string, err error)
}

// Entrega es el desenlace de entregar varias tareas en un solo PR.
type Entrega struct {
	Tareas   []Resultado `json:"tareas"`
	Pasos    []Paso      `json:"pasos"`
	Aprobado bool        `json:"aprobado"`
	PR       string      `json:"pr,omitempty"`
	Rama     string      `json:"rama,omitempty"`
	// Integrado dice si el PR llegó a mergearse. Un PR abierto y sin
	// integrar es un desenlace válido: el revisor pudo vetarlo.
	Integrado bool `json:"integrado,omitempty"`
	// Fallo es lo que frenó la entrega al probar el conjunto integrado
	// (pasos integradas y aceptación); nil si frenó otra cosa o no frenó.
	Fallo *Fallo `json:"fallo,omitempty"`
}

// Fallo es un comando que falló sobre el conjunto integrado. Es lo que
// `loom reparar` necesita para reabrir la tarea responsable.
type Fallo struct {
	Paso    string `json:"paso"`
	Comando string `json:"comando"`
	Salida  string `json:"salida"`
	// Pruebas son los archivos de prueba que fallaron, según la salida.
	Pruebas []string `json:"pruebas,omitempty"`
	// Nombres son las pruebas que fallaron tal como las nombró el runner.
	Nombres []string `json:"nombres,omitempty"`
	// Selectores es lo que se le agrega al comando para correr solo lo
	// que falló, en la forma que su runner entiende.
	Selectores []string `json:"selectores,omitempty"`
}

var (
	pruebaFallida = regexp.MustCompile(`FAIL\s+(\S+)`)
	// "test_x (paquete.modulo.Clase.test_x)" de unittest y Django; antes
	// de Python 3.11 el paréntesis no trae el método
	nombreUnittest = regexp.MustCompile(`^(\w+) \(([\w.]+)\)$`)
)

// falloDe arma el Fallo de un comando: salida es el resumen que ve el
// humano y crudo la salida entera, de donde salen los nombres. dir es el
// worktree donde corrió, para resolver un módulo de Python a su archivo.
//
// ponytail: go test da el nombre de la función, no un archivo ni algo que
// pegar al comando; queda en Nombres y la tarea se elige a mano. Buscar
// `func TestX(` en el árbol si hace falta.
func falloDe(dir, paso, comando, salida, crudo string) *Fallo {
	f := &Fallo{Paso: paso, Comando: comando, Salida: salida}
	agregar := func(lista *[]string, v string) {
		if v != "" && !slices.Contains(*lista, v) {
			*lista = append(*lista, v)
		}
	}
	for _, m := range pruebaFallida.FindAllStringSubmatch(salida, -1) {
		if task.EsArchivoDePrueba(m[1]) {
			agregar(&f.Pruebas, m[1])
			agregar(&f.Selectores, m[1])
		}
	}
	nombres := make([]string, 0)
	for n := range pruebasFallidas(crudo) {
		nombres = append(nombres, n)
	}
	sort.Strings(nombres)
	for _, n := range nombres {
		agregar(&f.Nombres, n)
		if archivo, _, ok := strings.Cut(n, "::"); ok && task.EsArchivoDePrueba(archivo) { // pytest
			agregar(&f.Pruebas, archivo)
			agregar(&f.Selectores, n)
		} else if m := nombreUnittest.FindStringSubmatch(n); m != nil {
			id := m[2]
			if !strings.HasSuffix(id, "."+m[1]) {
				id += "." + m[1]
			}
			agregar(&f.Pruebas, archivoDeModulo(dir, id))
			agregar(&f.Selectores, id)
		} else if archivo := strings.Fields(n)[0]; task.EsArchivoDePrueba(archivo) { // jest, vitest
			agregar(&f.Pruebas, archivo)
			agregar(&f.Selectores, archivo)
		}
	}
	return f
}

// archivoDeModulo devuelve el .py que existe en dir para el prefijo más
// largo de un id con puntos ("a.b.Clase.test_x" → "a/b.py"), o "".
func archivoDeModulo(dir, id string) string {
	partes := strings.Split(id, ".")
	for n := len(partes); n > 0; n-- {
		rel := filepath.Join(partes[:n]...) + ".py"
		if info, err := os.Stat(filepath.Join(dir, rel)); err == nil && !info.IsDir() {
			return filepath.ToSlash(rel)
		}
	}
	return ""
}

// PrimerMotivo devuelve el motivo del primer paso o tarea que frenó.
func (e Entrega) PrimerMotivo() string {
	for _, r := range e.Tareas {
		if !r.Aprobado {
			return r.ID + " · " + r.PrimerMotivo()
		}
	}
	for _, p := range e.Pasos {
		if !p.OK {
			return p.Detalle
		}
	}
	return ""
}

// EntregarTodas pasa cada tarea por su esclusa de salida y, si todas la
// superan, junta sus commits en una sola rama lineal y abre UN PR.
//
// Es la diferencia entre N pull requests que se pisan entre sí y uno
// limpio: cada tarea aporta exactamente un commit (la esclusa individual
// ya aplanó sus wip), en orden de dependencia, sin merges de por medio.
// La suite del proyecto corre una vez más sobre el conjunto ya integrado,
// que es lo único capaz de detectar que dos tareas verdes por separado se
// rompen juntas.
func EntregarTodas(ctx context.Context, o OpcionesEntrega) Entrega {
	var e Entrega
	// lo que quedó probado se guarda, pase o no: `loom archive` solo
	// archiva un feature cuya última entrega salió en verde
	defer func() { _ = guardarAceptacion(o.Root, aceptacionDe(o, e)) }()
	if o.Base == "" {
		o.Base = "HEAD"
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	apuntar := func(p Paso) {
		e.Pasos = append(e.Pasos, p)
		if o.Progreso != nil {
			o.Progreso(p)
		}
	}

	ordenadas, err := ordenTopologico(o.Tareas)
	if err != nil {
		apuntar(Paso{"orden", false, err.Error()})
		return e
	}

	// 1. la esclusa de salida completa de cada tarea, en seco: ninguna
	//    entra al PR si no pasa sola su propio control de calidad.
	//
	//    La esclusa aplana la rama de cada cuarto, y ese commit nuevo es
	//    el que se integra abajo. Pero la rama no puede quedarse así: las
	//    tareas que dependen de ella se construyeron sobre sus commits
	//    originales, y con otro commit en su lugar `run` ya no puede
	//    juntarlas ("CONFLICTO (agregar/agregar)" en cada archivo del
	//    esqueleto). En una corrida real, una entrega frenada dejó así las 10
	//    ramas y la tarea que faltaba no pudo correr. Al salir, cada rama
	//    vuelve a su punta.
	//
	//    Las puntas van además a disco: un Ctrl+C no corre el defer, y
	//    la entrega siguiente (o `run`) las restaura con RestaurarPuntas.
	if hechas, dudosas := RestaurarPuntas(o.Root); len(hechas)+len(dudosas) > 0 {
		apuntar(avisoPuntas(hechas, dudosas))
	}
	puntas := map[string]string{}
	defer func() {
		for path, punta := range puntas {
			_, _ = gitRun(path, "rebase", "--abort")
			_, _ = gitRun(path, "reset", "--hard", punta)
		}
		_ = os.Remove(archivoPuntas(o.Root))
	}()
	var conCambios []task.Task
	for _, t := range ordenadas {
		roomPath := roomPathDe(o.Root, t.ID)
		if punta, err := gitRun(roomPath, "rev-parse", "HEAD"); err == nil {
			puntas[roomPath] = strings.TrimSpace(punta)
			guardarPuntas(o.Root, puntas)
		}
		// cada tarea se aplana contra su propio punto de partida, no
		// contra la rama base: así su commit lleva solo lo suyo
		baseTarea := o.Commits[t.ID]
		if baseTarea == "" {
			baseTarea = baseDelta(roomPath, t.ID, o.Base) // cuartos anteriores a que se anotara
		}
		if baseTarea == "" {
			baseTarea = o.Base
		}
		// verde sin cambios propios (un esqueleto que solo planeó cambios
		// a código existente): no hay nada que integrar, no es un fallo
		if _, err := gitRun(roomPath, "diff", "--quiet", baseTarea, "HEAD"); err == nil {
			apuntar(Paso{"esclusa " + t.ID, true, "sin cambios propios · se omite"})
			continue
		}
		conCambios = append(conCambios, t)
		r := Run(ctx, Opciones{
			Root:    o.Root,
			Room:    room.Room{ID: t.ID, Path: roomPath, Rama: room.Branch(t.ID)},
			Task:    t,
			Config:  o.Config,
			Modelo:  o.Modelos[t.ID],
			Base:    baseTarea,
			Timeout: o.Timeout,
			DryRun:  true, // el PR lo abre la entrega, no cada tarea
			// la suite completa la corre integradas, sobre la base actual
			SuiteAlIntegrar: true,
		})
		e.Tareas = append(e.Tareas, r)
		if !r.Aprobado {
			apuntar(Paso{"esclusa " + t.ID, false, r.PrimerMotivo()})
			return e
		}
		apuntar(Paso{"esclusa " + t.ID, true, "lista para integrar"})
	}
	ordenadas = conCambios
	if len(ordenadas) == 0 {
		apuntar(Paso{"integrar", false, idioma.T("ninguna tarea tiene cambios propios · nada que entregar")})
		return e
	}

	// 2. una rama de entrega limpia desde la base
	// pedida local, sale de la base local: es a donde se va a integrar
	// por fast-forward, y puede ir por delante de origin
	target := o.Base
	if o.Config.Entrega != config.EntregaLocal {
		if _, err := gitRun(o.Root, "rev-parse", "--verify", "--quiet", "origin/"+o.Base); err == nil {
			target = "origin/" + o.Base
		}
	}
	path := roomPathDe(o.Root, "_entrega")
	if err := limpiarEntrega(o.Root, path); err != nil {
		apuntar(Paso{"rama de entrega", false, err.Error()})
		return e
	}
	if out, err := gitRun(o.Root, "worktree", "add", path, "-b", RamaEntrega, target); err != nil {
		apuntar(Paso{"rama de entrega", false, tail(out)})
		return e
	}
	local := prLocal(o.Root, o.Config)
	defer func() {
		if !e.Aprobado || o.DryRun {
			return
		}
		if local && !e.Integrado {
			// la rama ES el PR local: se quita solo la carpeta, para que
			// la rama pueda revisarse o mergearse desde el repo
			_, _ = gitRun(o.Root, "worktree", "remove", "--force", path)
			return
		}
		_ = limpiarEntrega(o.Root, path)
	}()
	e.Rama = RamaEntrega
	apuntar(Paso{"rama de entrega", true, RamaEntrega + " desde " + target})

	// 3. un commit por tarea, en orden de dependencia
	for _, t := range ordenadas {
		// cherry-pick crea un commit, así que necesita identidad: sin
		// ella git muere con "empty ident name" en cualquier maquina que
		// no tenga user.name configurado (un runner de CI, por ejemplo)
		args := append(identity(path), "cherry-pick", room.Branch(t.ID))
		if out, err := gitRun(path, args...); err != nil {
			// un cherry-pick falla por conflicto, pero tambien por
			// cosas que no tienen nada que ver con el codigo. Culpar
			// siempre al solapamiento manda al usuario a revisar unos
			// tocar_solo que estan bien.
			conflictos := unmerged(path)
			_, _ = gitRun(path, "cherry-pick", "--abort")
			detalle := "no se pudo integrar · " + tail(out)
			if len(conflictos) > 0 {
				detalle = "choca con el trabajo de una tarea anterior en " + unir(conflictos) +
					" · sus alcances (tocar_solo) se solapan"
			}
			apuntar(Paso{"integrar " + t.ID, false, detalle})
			return e
		}
	}
	apuntar(Paso{"integrar", true, fmt.Sprintf(idioma.T("%d commits, uno por tarea"), len(ordenadas))})

	// 4. la suite completa sobre el conjunto ya integrado. La rama de
	// entrega es un worktree nuevo: sin instalar, la suite de un
	// monorepo falla por .venv o node_modules y no por el código
	// y hereda el entorno de los cuartos que integra: llegan por
	// cherry-pick, así que ninguno es ancestro y de cero pip resuelve
	// distinto que donde las tareas ya corrieron
	ids := make([]string, len(ordenadas))
	for i, t := range ordenadas {
		ids[i] = t.ID
	}
	if err := room.InstalarDependencias(ctx, path, ids...); err != nil {
		apuntar(Paso{"integradas", false, err.Error()})
		return e
	}
	pruebas := strings.TrimSpace(o.Config.Pruebas)
	if pruebas == "" {
		apuntar(Paso{"integradas", true, idioma.T("sin comando de pruebas en config.yml · no se verificó el conjunto")})
	} else if crudo, salida, ok := correrPruebasCrudo(ctx, path, pruebas, o.Timeout); !ok {
		// una suite que ya estaba roja en la base no dice nada del
		// conjunto por su código de salida: lo que frena son los fallos
		// que la base no tenía
		detalle, mismos := mismosFallosQueLaBase(ctx, path, target, pruebas, o.Timeout, crudo)
		if !mismos {
			e.Fallo = falloDe(path, "integradas", pruebas, salida, crudo)
			// sin el nombre de la prueba, la aserción sola no dice dónde mirar
			if detalle == "" && len(e.Fallo.Nombres) > 0 {
				detalle = unir(e.Fallo.Nombres) + " · "
			}
			apuntar(Paso{"integradas", false,
				"las tareas pasan por separado pero el conjunto falla · " + detalle + salida})
			return e
		}
		apuntar(Paso{"integradas", true, detalle})
	} else {
		apuntar(Paso{"integradas", true, pruebas})
	}
	for _, command := range o.Acceptance {
		if crudo, salida, ok := correrPruebasCrudo(ctx, path, command, o.Timeout); !ok {
			// vitest sin archivos cierra con su lista de exclusiones: el
			// motivo real es que la prueba no existe en lo entregado
			if f := task.PruebaSinEscribir(path, command); f != "" {
				salida = "la prueba " + f + " no existe · ninguna tarea entregada la escribe (¿se borró o no corrió la tarea final?)"
			} else if avisos := spec.AceptacionSinPrueba(path, spec.Spec{Acceptance: []spec.Acceptance{{Command: command}}}, nil); len(avisos) > 0 {
				// un id de módulo que no existe se ve como "errors=1"
				salida = avisos[0].Message
			}
			e.Fallo = falloDe(path, "aceptación", command, salida, crudo)
			if len(e.Fallo.Nombres) > 0 {
				salida = unir(e.Fallo.Nombres) + " · " + salida
			}
			apuntar(Paso{"aceptación", false, command + " · " + salida})
			return e
		}
		apuntar(Paso{"aceptación", true, command})
	}

	// capturas del resultado integrado para el humano: el juicio estético
	// final es suyo, y las pruebas no lo ven. Nunca frena la entrega
	var fotos []string
	if !o.Config.Pantallas.Vacia() && !o.DryRun {
		dir := filepath.Join(o.Root, ".loom", "pr", "capturas")
		_ = os.RemoveAll(dir)
		var err error
		if fotos, err = capturas.Tomar(ctx, path, o.Config.Pantallas, puertoLibre(), room.Entorno(path), dir); err != nil {
			apuntar(Paso{"capturas", true, idioma.T("sin capturas · ") + err.Error()})
		} else {
			apuntar(Paso{"capturas", true, fmt.Sprintf(idioma.T("%d pantallas en %s"), len(fotos), dir)})
		}
	}

	// 5. un solo PR
	cuerpo := cuerpoEntrega(ordenadas, e.Tareas) + seccionCapturas(fotos)
	titulo := o.Titulo
	if titulo == "" && len(ordenadas) > 0 {
		titulo = ordenadas[0].Titulo
	}
	if o.DryRun {
		apuntar(Paso{"pr", true, idioma.T("dry-run · sin PR · rama ") + RamaEntrega})
		e.Aprobado = true
		return e
	}
	var url string
	if local {
		url, err = abrirPRLocal(o.Root, RamaEntrega, o.Base, titulo, cuerpo)
	} else {
		url, err = abrirPREntrega(ctx, o.Root, path, o.Base, titulo, cuerpo)
		// con remoto pero sin poder abrir el PR (gh sin instalar, con otra
		// cuenta, sin permiso de push): la entrega ya pasó todas las
		// compuertas y no se tira; queda como PR local y se dice por qué
		if err != nil {
			motivo := err.Error()
			if url, err = abrirPRLocal(o.Root, RamaEntrega, o.Base, titulo, cuerpo); err == nil {
				local = true
				apuntar(Paso{"pr remoto", true, idioma.T("no se abrió en el remoto · ") + motivo + " · queda como PR local"})
			}
		}
	}
	if err != nil {
		apuntar(Paso{"pr", false, err.Error()})
		return e
	}
	apuntar(Paso{"pr", true, url})
	e.PR, e.Aprobado = url, true

	if o.Revisor != nil {
		sinCambios, ok := revisarEntrega(ctx, o, path, target, url, ordenadas, apuntar)
		if !ok || !sinCambios || !o.Integrar {
			// el PR queda abierto con el informe dentro. Aprobar es del
			// humano: el revisor informa, no decide.
			return e
		}
	} else if !o.Integrar {
		return e
	}

	if o.Integrar {
		integrar, como := func() error { return mergearPR(ctx, o.Root, path, o.Base, url) }, "mergeado por rebase · un commit por tarea"
		if local {
			integrar, como = func() error { return integrarLocal(ctx, o.Root, path, o.Base) }, "fast-forward local · un commit por tarea"
		}
		if err := integrar(); err != nil {
			apuntar(Paso{"integrar en " + o.Base, false, err.Error()})
			return e
		}
		apuntar(Paso{"integrar en " + o.Base, true, como})
		e.Integrado = true
	}

	// los cuartos ya entregaron: liberarlos deja el repo limpio
	for _, t := range ordenadas {
		_ = room.Destroy(ctx, o.Root, t.ID)
	}
	_ = room.Destroy(ctx, o.Root, "_integra")
	return e
}

// revisarEntrega pide el informe del revisor y lo deja como comentario
// en el PR. Devuelve si no pidió cambios, y si la revisión se pudo hacer.
//
// Falla cerrado: lo que no se pudo revisar no se da por revisado. El PR
// queda abierto con el motivo, para que quien aprueba sepa que nadie
// miró el diff.
func revisarEntrega(ctx context.Context, o OpcionesEntrega, path, target, url string, tareas []task.Task, apuntar func(Paso)) (sinCambios, ok bool) {
	diff, err := gitRun(path, "diff", target+"..HEAD")
	if err != nil {
		apuntar(Paso{"revisión", false, idioma.T("no se pudo leer el diff · ") + tail(diff)})
		return false, false
	}
	sinCambios, informe, resumen, err := o.Revisor.Revisar(ctx, diff, tareas)
	if err != nil {
		apuntar(Paso{"revisión", false, err.Error() + " · el PR queda abierto sin revisar"})
		return false, false
	}
	comentar := func() error { return comentarPR(ctx, o.Root, url, informe) }
	if strings.HasPrefix(url, PRLocal) {
		comentar = func() error { return comentarPRLocal(o.Root, RamaEntrega, informe) }
	}
	if err := comentar(); err != nil {
		// el informe es el producto de la revisión: si no llega al PR,
		// quien aprueba no lo va a ver
		apuntar(Paso{"revisión", false, idioma.T("no se pudo publicar el informe en el PR · ") + err.Error()})
		return sinCambios, false
	}
	apuntar(Paso{"revisión", true, resumen})
	return sinCambios, true
}

// baseDelta devuelve el commit desde el que empieza el trabajo propio de
// una tarea: el padre del primer commit que escribió loom en ese
// cuarto (la suite del examinador, o el primer `wip:`).
//
// Hace falta porque un cuarto de la segunda oleada nace desde la rama de
// integración y por tanto YA contiene el trabajo de la oleada anterior.
// Aplanarlo contra la rama base metería ese trabajo ajeno dentro de su
// propio commit, y al juntar los commits en la rama de entrega el segundo
// volvería a añadir los archivos del primero: conflicto garantizado.
// Cadena vacía si no se puede determinar; el llamador cae en la base.
func baseDelta(roomPath, id, base string) string {
	out, err := gitRun(roomPath, "log", "--reverse", "--format=%H%x00%s", "HEAD")
	if err != nil {
		return ""
	}
	prefijoWip := "wip: " + id + " "
	for _, linea := range strings.Split(out, "\n") {
		hash, subject, ok := strings.Cut(strings.TrimSpace(linea), "\x00")
		if !ok {
			continue
		}
		if !strings.HasPrefix(subject, "exam:") && !strings.HasPrefix(subject, prefijoWip) {
			continue
		}
		padre, err := gitRun(roomPath, "rev-parse", "--verify", "--quiet", hash+"^")
		if err != nil {
			return "" // el trabajo arranca en el commit raíz
		}
		return strings.TrimSpace(padre)
	}

	// Sin marcadores, la esclusa ya paso por aqui y aplano la rama en un
	// solo commit: el trabajo propio de la tarea es justo ese commit. Sin
	// este caso, una segunda pasada media el cuarto entero contra la rama
	// base y frenaba por un presupuesto que nadie habia excedido.
	if unSoloCommit(roomPath, base) {
		if padre, err := gitRun(roomPath, "rev-parse", "--verify", "--quiet", "HEAD^"); err == nil {
			return strings.TrimSpace(padre)
		}
	}
	return ""
}

// unSoloCommit reporta si la rama tiene exactamente un commit por encima
// de base, que es la forma que deja `aplanar`.
func unSoloCommit(roomPath, base string) bool {
	out, err := gitRun(roomPath, "rev-list", "--count", base+"..HEAD")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "1"
}

// roomPathDe devuelve la ruta del cuarto de un id.
func roomPathDe(root, id string) string { return filepath.Join(room.Dir(root), id) }

// limpiarEntrega borra el worktree y la rama de entrega de una corrida
// anterior, para que `worktree add` no choque.
func limpiarEntrega(root, path string) error {
	_, _ = gitRun(root, "worktree", "remove", "--force", path)
	_, _ = gitRun(root, "worktree", "prune")
	if _, err := gitRun(root, "rev-parse", "--verify", "--quiet", "refs/heads/"+RamaEntrega); err != nil {
		return nil
	}
	if out, err := gitRun(root, "branch", "-D", RamaEntrega); err != nil {
		return fmt.Errorf(idioma.T("no se pudo borrar la rama de entrega · %s"), tail(out))
	}
	return nil
}

// correrPruebas corre el comando de pruebas del proyecto en dir.
func correrPruebas(ctx context.Context, dir, cmdStr string, timeout time.Duration) (string, bool) {
	_, resumen, ok := correrPruebasCrudo(ctx, dir, cmdStr, timeout)
	return resumen, ok
}

// correrPruebasCrudo es correrPruebas con la salida entera además del
// resumen: comparar fallos contra la base necesita los nombres.
func correrPruebasCrudo(ctx context.Context, dir, cmdStr string, timeout time.Duration) (crudo, resumen string, ok bool) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// el conjunto y la aceptación del feature: FS aislado, red abierta
	cmd := task.Shell(ctx, dir, cmdStr, true)
	cmd.Env = append(os.Environ(), room.Entorno(dir)...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Sprintf(idioma.T("las pruebas tardaron más de %s"), timeout), false
	}
	if err != nil {
		return string(out), resumenPruebas(string(out)), false
	}
	return string(out), "", true
}

// reFallo caza el nombre de cada prueba fallida en los formatos comunes:
// unittest/Django ("FAIL: test_x (mod.Clase)", "ERROR: …"), pytest
// ("FAILED ruta::test - motivo"), go ("--- FAIL: TestX (0.00s)") y
// jest/vitest ("FAIL src/x.test.ts").
// ponytail: por texto; un runner con otro formato no da nombres y la
// compuerta sigue frenando como antes.
var (
	reFallo    = regexp.MustCompile(`(?m)^\s*(?:--- )?(?:FAIL|FAILED|ERROR):?[ \t]+(\S.*)$`)
	reDuracion = regexp.MustCompile(`\s*\(?[0-9.]+m?s\)?$`)
)

// pruebasFallidas devuelve el conjunto de pruebas que fallaron según la
// salida de un runner. Vacío si no reconoce ninguna.
func pruebasFallidas(salida string) map[string]bool {
	fallos := map[string]bool{}
	for _, m := range reFallo.FindAllStringSubmatch(salida, -1) {
		nombre, _, _ := strings.Cut(m[1], " - ")
		// "FAILED (failures=2)" es el cierre de unittest, no una prueba
		if nombre = strings.TrimSpace(reDuracion.ReplaceAllString(nombre, "")); nombre != "" && !strings.HasPrefix(nombre, "(") {
			fallos[nombre] = true
		}
	}
	return fallos
}

// mismosFallosQueLaBase corre la suite sobre la base, en el mismo worktree
// (sus dependencias ya están), y reporta si todo lo que falla en el
// conjunto ya fallaba ahí. Un proyecto con la suite roja desde antes no
// podía entregar nunca; lo que el conjunto rompe de nuevo sigue frenando.
// Sin nombres que comparar no hay veredicto y se frena.
func mismosFallosQueLaBase(ctx context.Context, path, base, pruebas string, timeout time.Duration, crudo string) (string, bool) {
	conjunto := pruebasFallidas(crudo)
	if len(conjunto) == 0 {
		return "", false
	}
	if _, err := gitRun(path, "checkout", "--quiet", "--detach", base); err != nil {
		return "", false
	}
	enBase, _, ok := correrPruebasCrudo(ctx, path, pruebas, timeout)
	if _, err := gitRun(path, "checkout", "--quiet", RamaEntrega); err != nil || ok {
		return "", false
	}
	previos := pruebasFallidas(enBase)
	var nuevos []string
	for nombre := range conjunto {
		if !previos[nombre] {
			nuevos = append(nuevos, nombre)
		}
	}
	if len(nuevos) > 0 {
		sort.Strings(nuevos)
		return fmt.Sprintf(idioma.T("%d fallos que la base no tenía: %s · "), len(nuevos), unir(nuevos)), false
	}
	return fmt.Sprintf(idioma.T("%s · la suite ya fallaba en %s con esos mismos %d fallos y el conjunto no agrega ninguno"), pruebas, base, len(conjunto)), true
}

// ordenTopologico ordena las tareas para que ninguna llegue antes que
// aquella de la que depende. Un ciclo se reporta en vez de colgarse.
func ordenTopologico(tareas []task.Task) ([]task.Task, error) {
	porID := make(map[string]task.Task, len(tareas))
	for _, t := range tareas {
		porID[t.ID] = t
	}
	ids := make([]string, 0, len(tareas))
	for _, t := range tareas {
		ids = append(ids, t.ID)
	}
	sort.Strings(ids) // orden estable entre tareas sin dependencia mutua

	listo := map[string]bool{}
	var out []task.Task
	for len(out) < len(tareas) {
		avanzo := false
		for _, id := range ids {
			if listo[id] {
				continue
			}
			puede := true
			for _, d := range porID[id].DependeDe {
				// una dependencia fuera del lote ya está en la base
				if _, enLote := porID[d]; enLote && !listo[d] {
					puede = false
					break
				}
			}
			if !puede {
				continue
			}
			listo[id] = true
			out = append(out, porID[id])
			avanzo = true
		}
		if !avanzo {
			var faltan []string
			for _, id := range ids {
				if !listo[id] {
					faltan = append(faltan, id)
				}
			}
			return nil, fmt.Errorf(idioma.T("ciclo de dependencias entre %s · revisa depende_de"), strings.Join(faltan, ", "))
		}
	}
	return out, nil
}

// cuerpoEntrega arma el handoff del PR conjunto: una sección por tarea,
// con lo mismo que llevaría su PR individual.
func cuerpoEntrega(tareas []task.Task, resultados []Resultado) string {
	porID := make(map[string]Resultado, len(resultados))
	for _, r := range resultados {
		porID[r.ID] = r
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d tareas entregadas, un commit cada una.\n\n", len(tareas))
	for _, t := range tareas {
		r := porID[t.ID]
		fmt.Fprintf(&b, "---\n\n### %s · %s\n\n", t.ID, t.Titulo)
		if t.Porque != "" {
			fmt.Fprintf(&b, "%s\n\n", t.Porque)
		}
		fmt.Fprintf(&b, "%d líneas añadidas, %d quitadas.\n\n", r.LineasMas, r.LineasMenos)
		if t.Riesgos != "" {
			fmt.Fprintf(&b, "Riesgos: %s\n\n", t.Riesgos)
		}
		fmt.Fprintf(&b, "Verificar:\n```\n%s\n```\n\n", t.ListoCuando)
	}
	return b.String()
}

// abrirPREntrega sube la rama de entrega y abre el PR único.
func abrirPREntrega(ctx context.Context, root, path, base, titulo, cuerpo string) (string, error) {
	gh, err := exec.LookPath("gh")
	if err != nil {
		return "", fmt.Errorf(idioma.T("gh no está instalado · instálalo o usa --dry-run"))
	}
	if _, err := gitRun(root, "remote", "get-url", "origin"); err != nil {
		return "", fmt.Errorf(idioma.T("sin remoto origin · agrégalo con git remote add origin <url> o usa --dry-run"))
	}
	if out, err := gitRun(path, "push", "--force", "-u", "origin", RamaEntrega); err != nil {
		return "", fmt.Errorf(idioma.T("no se pudo subir la rama de entrega · %s"), tail(out))
	}
	if url := prExistente(ctx, root, gh, RamaEntrega); url != "" {
		return url, nil
	}

	f, err := os.CreateTemp("", "loom-entrega-*.md")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(cuerpo); err != nil {
		f.Close()
		return "", err
	}
	f.Close()

	cmd := exec.CommandContext(ctx, gh, "pr", "create", "--base", base, "--head", RamaEntrega, "--title", titulo, "--body-file", f.Name())
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		if url := prExistente(ctx, root, gh, RamaEntrega); url != "" {
			return url, nil
		}
		return "", fmt.Errorf(idioma.T("gh no pudo abrir el PR · %s"), tail(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// mergearPR integra el PR en la rama base por rebase, para que los
// commits por tarea entren tal cual: es lo que hace que el historial de
// la rama principal siga siendo bisectable, un commit por tarea.
//
// Si la base se movió desde que se abrió el PR, rebasea la rama de
// entrega sobre la base nueva, vuelve a subirla y reintenta una vez. Un
// conflicto real se reporta con los archivos, no con "no se pudo".
func mergearPR(ctx context.Context, root, path, base, url string) error {
	gh, err := exec.LookPath("gh")
	if err != nil {
		return errors.New(idioma.T("gh no está instalado"))
	}

	merge := func() (string, error) {
		cmd := exec.CommandContext(ctx, gh, "pr", "merge", url, "--rebase", "--delete-branch")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	salida, err := merge()
	if err == nil {
		return nil
	}

	// segundo intento: la base pudo avanzar entre abrir el PR y mergear
	if err := realinearConBase(ctx, root, path, base, true); err != nil {
		return fmt.Errorf("%s · %s", tail(salida), err)
	}
	if salida, err = merge(); err != nil {
		return fmt.Errorf(idioma.T("no se pudo integrar ni tras realinear con %s · %s"), base, tail(salida))
	}
	return nil
}

// realinearConBase rebasea la rama de entrega sobre la base actual y, con
// subir, la vuelve a subir. Sin subir es un PR local: se rebasea sobre la
// base local, que es a donde va el fast-forward. Devuelve un error que
// nombra los archivos si el conflicto es real.
func realinearConBase(ctx context.Context, root, path, base string, subir bool) error {
	target := base
	if subir {
		traer(root)
		if _, err := gitRun(root, "rev-parse", "--verify", "--quiet", "origin/"+base); err == nil {
			target = "origin/" + base
		}
	}
	if salida, err := gitRun(path, "rebase", target); err != nil {
		conflictos := unmerged(path)
		_, _ = gitRun(path, "rebase", "--abort")
		if len(conflictos) > 0 {
			return fmt.Errorf(idioma.T("%s cambió y choca en %s · resuélvelo a mano"), target, unir(conflictos))
		}
		return fmt.Errorf(idioma.T("no se pudo rebasear sobre %s · %s"), target, tail(salida))
	}
	if !subir || sinRemoto(root) {
		return nil // PR local: no hay a dónde subirla
	}
	if salida, err := gitRun(path, "push", "--force", "origin", RamaEntrega); err != nil {
		return fmt.Errorf(idioma.T("no se pudo subir la rama realineada · %s"), tail(salida))
	}
	return nil
}

// comentarPR deja el veredicto del revisor en el propio PR, para que la
// razón viva donde alguien la va a leer y no solo en la terminal.
func comentarPR(ctx context.Context, root, url, cuerpo string) error {
	gh, err := exec.LookPath("gh")
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, gh, "pr", "comment", url, "--body", cuerpo)
	cmd.Dir = root
	return cmd.Run()
}

// seccionCapturas agrega las fotos al PR. Las rutas son relativas al
// markdown de .loom/pr: se ven en el PR local; en GitHub quedan como
// referencia a los archivos de la máquina.
func seccionCapturas(fotos []string) string {
	if len(fotos) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Capturas (celular)\n\n")
	for _, f := range fotos {
		fmt.Fprintf(&b, "![%s](capturas/%s)\n", filepath.Base(f), filepath.Base(f))
	}
	return b.String()
}

// puertoLibre pide al sistema un puerto sin uso para levantar la app.
func puertoLibre() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 18999
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
