// Package recurse implementa la ejecución recursiva de tareas: una
// tarea marcada `recursivo: true` no la resuelve un solo intento de
// agente — se reparte en subtareas reales, cada una con su propio
// contrato y su propio `listo_cuando`, que corren en cuartos anidados
// DENTRO del cuarto de la tarea padre.
//
// Un cuarto ya es un worktree completo del repo: los worktrees no
// comparten archivos sin commitear entre sí, así que un cuarto anidado
// adentro de otro cuarto sale gratis — "git propio" para cada subtarea
// sin reinventar nada, mismo mecanismo de siempre (internal/room).
//
// Agent implementa loop.Agent: para el bucle padre, la descomposición
// entera es "lo que hizo el agente en este intento". El bucle no cambia
// una línea — sigue verificando `listo_cuando` del padre después, con
// código, nunca con el juicio del modelo (manifiesto regla 3).
//
// Las subtareas independientes corren en PARALELO, en oleadas limitadas
// por `subagentes` en config.yml: el modelo barato se ocupa de las hojas
// chicas y el orquestador solo interviene cuando algo falla. Una hoja
// roja no aborta el árbol: se escala de modelo, se le pregunta al
// orquestador qué hacer y se sigue con las demás. Solo cuando las hojas
// rojas no se pueden salvar y el `listo_cuando` del padre sigue fallando
// (o se agota el presupuesto de tokens), la recursión se detiene con un
// loop.ErrDetener — sin quemar los intentos restantes del padre.
package recurse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Pastranauwu/loom/internal/budget"
	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/gate"
	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/plan"
	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/ventanas"
)

// LimiteReplan topa cuántas veces el orquestador puede reescribir el
// contrato de una misma subtarea. Una replan fallida otra vez no se
// vuelve a consultar: la hoja queda roja y se sigue con las demás.
const LimiteReplan = 1

// muCuarto serializa la creación y destrucción de cuartos anidados: git
// worktree add/remove del mismo repo en paralelo choca por los locks
// internos. La ejecución del agente dentro del cuarto no se serializa.
var muCuarto sync.Mutex

// autoSubagentes resuelve el paralelismo de la recursión cuando config no
// lo fija: tantas subtareas en paralelo como haya en la oleada, topado a
// 8. Sin esto la descomposición corría las hojas independientes en fila.
func autoSubagentes(tareas int) int {
	if tareas > 8 {
		return 8
	}
	if tareas < 1 {
		return 1
	}
	return tareas
}

// Agent descompone una tarea recursiva y ejecuta sus subtareas.
type Agent struct {
	Cfg          config.Config
	Constitucion string
	Planificador plan.Generador // rol planificador (modelo caro): descompone y supervisa
	Ejecutor     loop.Agent     // agente hoja (modelo barato) para subtareas no recursivas
	// EjecutorPara, si está, da el agente hoja que sabe correr un modelo:
	// con modelos: mezclando CLIs, la hoja escalada puede ser de otro CLI.
	EjecutorPara   func(modelo string) loop.Agent
	ModeloEjecutor string
	Task           task.Task // la tarea recursiva que este Agent resuelve
	Profundidad    int       // 0 = raíz
	// Presupuesto es el tope de gasto compartido de la corrida. Las
	// hojas lo gastan con sus intentos; al agotarse, la recursión corta.
	Presupuesto *budget.Contador
	// Ventanas es el ledger de ventanas rodantes (5h/semanal/mensual) de
	// la cuenta. Las hojas registran su gasto; al pasar un tope
	// configurado, la hoja corta y la recursión se detiene.
	Ventanas *ventanas.Registro

	// Root es la raíz del proyecto (no el cuarto): ahí viven arbol.json,
	// y desde aquí los latidos y attempts de las subtareas, para que
	// sobrevivan a que el cuarto de la subtarea se destruya y para que
	// board/standup puedan verlas en vivo.
	Root string
	// RaizID es la tarea de nivel 0 que arrancó la recursión — todo nodo,
	// a cualquier profundidad, se guarda bajo su arbol.json, así
	// board/standup/TUI leen un solo archivo por tarea raíz.
	RaizID string
}

// Name identifica al agente en attempts.jsonl y en los logs.
func (a Agent) Name() string { return "recursivo" }

// resultadoSub es el desenlace de una subtarea ya resuelta. Si verde, su
// cuarto sigue vivo (con su rama) esperando la integración; si no, el
// cuarto ya se destruyó.
type resultadoSub struct {
	sub    task.Task
	verde  bool
	modelo string
	tk     loop.Tokens
	motivo string
}

// Run descompone la tarea en subtareas, las corre en oleadas paralelas
// dentro de cuartos anidados y las integra a la rama de este cuarto. No
// abre PR ni decide nada por juicio propio: el bucle del padre corre
// `listo_cuando` después de esto, exactamente igual que si un solo agente
// hubiera escrito el código a mano.
//
// El desenlace de una hoja roja sigue esta escalera, en orden:
//  1. se escala al siguiente modelo más pesado (`modelos` en config) y
//     se reintenta reusando el mismo cuarto y su trabajo parcial;
//  2. si sigue roja y hay trabajo real, el orquestador (Planificador)
//     recibe un brief compacto y puede replanificar la subtarea, que se
//     reintenta desde un cuarto nuevo;
//  3. si queda roja, se registra en el árbol y se CONTINÚA con las
//     demás subtareas — una hoja no mata el trabajo de sus hermanas.
//
// Al final, si quedaron hojas rojas, se corre el `listo_cuando` del
// padre: si pasó, las hojas rojas eran opcionales y se devuelve éxito;
// si no, se devuelve un loop.ErrDetener con el motivo exacto.
func (a Agent) Run(ctx context.Context, req loop.Request) (loop.Result, error) {
	limite := a.Task.LimiteSubtareas
	if limite < 1 {
		limite = task.DefaultLimiteSubtareas
	}

	borradores, err := a.descomponer(ctx)
	if err != nil {
		return loop.Result{}, fmt.Errorf(idioma.T("descomposición de %s falló · %s"), a.Task.ID, err)
	}
	if len(borradores) > limite {
		return loop.Result{}, fmt.Errorf(idioma.T("descomposición de %s propuso %d subtareas, el límite es %d · baja el alcance o sube limite_subtareas"), a.Task.ID, len(borradores), limite)
	}

	var subs []task.Task
	for i, b := range borradores {
		sub, err := a.tareaDesdeBorrador(b, i+1)
		if err != nil {
			return loop.Result{}, fmt.Errorf(idioma.T("subtarea %d de %s inválida · %s"), i+1, a.Task.ID, err)
		}
		subs = append(subs, sub)
	}

	// esclusa de entrada en serie: la que no pasa se rechaza antes de
	// gastar un token, mismo criterio que loom run
	roja := map[string]string{} // id -> motivo
	aprobadas := make([]task.Task, 0, len(subs))
	for _, s := range subs {
		gres := gate.Run(ctx, req.RoomPath, a.Cfg, s, nil, gate.DefaultTimeout)
		if !gres.Aprobada {
			roja[s.ID] = "rechazada en la esclusa de entrada · " + gres.PrimerMotivo()
			a.registrar(s, false, 0, roja[s.ID], loop.Tokens{}, "")
			continue
		}
		aprobadas = append(aprobadas, s)
	}

	subagentes := a.Cfg.Subagentes
	if subagentes < 1 {
		subagentes = autoSubagentes(len(aprobadas))
	}

	verde := map[string]bool{}
	var tokens loop.Tokens
	pendientes := aprobadas

	for len(pendientes) > 0 {
		var ola []task.Task
		for _, s := range pendientes {
			if depsVerdes(s.DependeDe, verde) {
				ola = append(ola, s)
			}
		}
		if len(ola) == 0 {
			// solo quedan bloqueadas por dependencias que no salieron
			for _, s := range pendientes {
				roja[s.ID] = "bloqueada · depende de " + strings.Join(depsFaltantes(s.DependeDe, verde), ", ") + " que no salió verde"
				a.registrar(s, false, 0, roja[s.ID], loop.Tokens{}, "")
			}
			break
		}

		// las solapadas en tocar_solo no corren juntas: la ola consume el
		// subconjunto sin cruce y el resto espera a la siguiente pasada
		batch, _ := separarSolapadas(ola)

		results := correrOla(ctx, req, a, batch, subagentes)
		for _, r := range results {
			tokens.Entrada += r.tk.Entrada
			tokens.Salida += r.tk.Salida
			if r.verde {
				verde[r.sub.ID] = true
			} else {
				roja[r.sub.ID] = r.motivo
			}
		}

		// integrar verdes en orden de id: una sola rama que va acumulando
		sort.Slice(results, func(i, j int) bool { return results[i].sub.ID < results[j].sub.ID })
		for _, r := range results {
			if !r.verde {
				continue
			}
			if err := integrar(req.RoomPath, room.Branch(r.sub.ID), r.sub.ID); err != nil {
				delete(verde, r.sub.ID)
				roja[r.sub.ID] = "no se pudo integrar · " + err.Error()
				a.registrar(r.sub, false, 0, roja[r.sub.ID], loop.Tokens{}, "")
				continue
			}
			muCuarto.Lock()
			_ = room.Destroy(ctx, req.RoomPath, r.sub.ID)
			muCuarto.Unlock()
		}

		// recomputar pendientes: lo que no quedó ni verde ni roja
		pendientes = pendientes[:0]
		for _, s := range aprobadas {
			if !verde[s.ID] && roja[s.ID] == "" {
				pendientes = append(pendientes, s)
			}
		}

		if a.Presupuesto != nil && a.Presupuesto.Agotado() {
			for _, s := range pendientes {
				roja[s.ID] = loop.MotivoPresupuesto
				a.registrar(s, false, 0, roja[s.ID], loop.Tokens{}, "")
			}
			pendientes = pendientes[:0]
			break
		}
	}

	if len(roja) == 0 {
		return loop.Result{ExitCode: 0, Tokens: tokens}, nil
	}
	if a.Presupuesto != nil && a.Presupuesto.Agotado() {
		return loop.Result{Tokens: tokens}, &loop.ErrDetener{Motivo: loop.MotivoPresupuesto}
	}
	// el padre puede haber quedado verde pese a las hojas rojas: la
	// última palabra la tiene listo_cuando, no el árbol
	if listoPadreVerde(ctx, req.RoomPath, a.Task.ListoCuando, a.Cfg.TimeoutPruebas) {
		return loop.Result{ExitCode: 0, Tokens: tokens}, nil
	}
	var partes []string
	for _, id := range sortedIDs(roja) {
		partes = append(partes, id+" · "+roja[id])
	}
	return loop.Result{Tokens: tokens}, &loop.ErrDetener{Motivo: "subtareas rojas: " + strings.Join(partes, " | ")}
}

// resolverSubtarea resuelve una subtarea completa dentro de su cuarto:
// intento con el modelo de su peso, escalada a un modelo más pesado si
// dejó trabajo y sigue roja, y dictamen del orquestador si el pesado
// tampoco pudo. Si queda verde, el cuarto queda vivo esperando que Run lo
// integre; si no, se destruye. Corre en una goroutine por hoja.
func (a Agent) resolverSubtarea(ctx context.Context, req loop.Request, sub task.Task) resultadoSub {
	modelo := a.modeloInicial(sub)

	if a.Presupuesto != nil && a.Presupuesto.Agotado() {
		return resultadoSub{sub: sub, modelo: modelo, motivo: loop.MotivoPresupuesto}
	}

	muCuarto.Lock()
	r, err := room.Create(ctx, req.RoomPath, sub.ID, "HEAD")
	muCuarto.Unlock()
	if err != nil {
		return resultadoSub{sub: sub, modelo: modelo, motivo: "no se pudo crear el cuarto · " + err.Error()}
	}
	a.registrar(sub, false, 0, "en curso · modelo "+modelo, loop.Tokens{}, modelo)

	outcome, agentErr, tk := a.correrSubtarea(ctx, req, r, sub, modelo)
	if agentErr != nil {
		a.registrar(sub, false, outcome.Intentos, "no pudo ejecutarse · "+agentErr.Error(), tk, modelo)
		muCuarto.Lock()
		_ = room.Destroy(ctx, req.RoomPath, sub.ID)
		muCuarto.Unlock()
		return resultadoSub{sub: sub, modelo: modelo, tk: tk, motivo: "no pudo ejecutarse · " + agentErr.Error()}
	}
	if outcome.Verde {
		a.registrar(sub, true, outcome.Intentos, "", tk, modelo)
		return resultadoSub{sub: sub, verde: true, modelo: modelo, tk: tk}
	}

	// hoja roja: solo merece escalar de modelo si de verdad dejó trabajo
	if m := a.Cfg.ModeloEscalado(sub.Peso, modelo); m != "" && !outcome.NoEscalar && huboTrabajo(rootPara(a, req), sub.ID) {
		outcome, agentErr, tk = a.correrSubtarea(ctx, req, r, sub, m)
		modelo = m
		if agentErr == nil && outcome.Verde {
			a.registrar(sub, true, outcome.Intentos, "", tk, modelo)
			return resultadoSub{sub: sub, verde: true, modelo: modelo, tk: tk}
		}
	}

	// el orquestador decide (replanificar) si el modelo pesado tampoco
	// pudo — es el único momento en que un modelo grande se entromete
	if a.Planificador != nil && huboTrabajo(rootPara(a, req), sub.ID) {
		if nuevo, ok := a.supervisar(ctx, sub, outcome.Pregunta); ok {
			muCuarto.Lock()
			gres := gate.Run(ctx, req.RoomPath, a.Cfg, nuevo, nil, gate.DefaultTimeout)
			_ = room.Destroy(ctx, req.RoomPath, sub.ID)
			var r2 room.Room
			if gres.Aprobada {
				r2, err = room.Create(ctx, req.RoomPath, sub.ID, "HEAD")
			}
			muCuarto.Unlock()
			if gres.Aprobada && err == nil {
				o2, ae2, tk2 := a.correrSubtarea(ctx, req, r2, nuevo, a.modeloInicial(nuevo))
				if ae2 == nil && o2.Verde {
					a.registrar(nuevo, true, o2.Intentos, "", tk2, a.modeloInicial(nuevo))
					return resultadoSub{sub: nuevo, verde: true, modelo: a.modeloInicial(nuevo), tk: tk2}
				}
				muCuarto.Lock()
				_ = room.Destroy(ctx, req.RoomPath, sub.ID)
				muCuarto.Unlock()
				outcome = o2
				tk = tk2
				modelo = a.modeloInicial(nuevo)
			}
		} else {
			muCuarto.Lock()
			_ = room.Destroy(ctx, req.RoomPath, sub.ID)
			muCuarto.Unlock()
		}
	} else {
		muCuarto.Lock()
		_ = room.Destroy(ctx, req.RoomPath, sub.ID)
		muCuarto.Unlock()
	}

	a.registrar(sub, false, outcome.Intentos, outcome.Pregunta, tk, modelo)
	return resultadoSub{sub: sub, modelo: modelo, tk: tk, motivo: outcome.Pregunta}
}

// correrOla ejecuta la ola en paralelo con hasta subagentes trabajadores.
// Cada hoja corre su propia escalera dentro de resolverSubtarea; los
// cuartos se crean y destruyen serializados por muCuarto.
func correrOla(ctx context.Context, req loop.Request, a Agent, batch []task.Task, subagentes int) []resultadoSub {
	if len(batch) == 0 {
		return nil
	}
	if subagentes > len(batch) {
		subagentes = len(batch)
	}
	jobs := make(chan task.Task)
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]resultadoSub, 0, len(batch))
	for i := 0; i < subagentes; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range jobs {
				r := a.resolverSubtarea(ctx, req, s)
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
			}
		}()
	}
	for _, s := range batch {
		jobs <- s
	}
	close(jobs)
	wg.Wait()
	return results
}

// separarSolapadas parte la ola en el subconjunto que puede correr junta
// (sin cruce de tocar_solo) y el resto que espera a la próxima pasada.
func separarSolapadas(ola []task.Task) (batch, restantes []task.Task) {
	sort.Slice(ola, func(i, j int) bool { return ola[i].ID < ola[j].ID })
	var aceptadas []task.Task
	for _, s := range ola {
		if c := gate.Cruce(s, aceptadas); c.OK {
			aceptadas = append(aceptadas, s)
			batch = append(batch, s)
			continue
		}
		restantes = append(restantes, s)
	}
	return batch, restantes
}

// depsVerdes reporta si todas las dependencias de una subtarea (ids de
// hermanas, mapeados de los índices de la descomposición) ya salieron.
func depsVerdes(deps []string, verde map[string]bool) bool {
	for _, d := range deps {
		if !verde[d] {
			return false
		}
	}
	return true
}

// depsFaltantes lista las dependencias que no salieron verdes.
func depsFaltantes(deps []string, verde map[string]bool) []string {
	var faltantes []string
	for _, d := range deps {
		if !verde[d] {
			faltantes = append(faltantes, d)
		}
	}
	return faltantes
}

// sortedIDs ordena las claves de un mapa de ids.
func sortedIDs(m map[string]string) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// registrar guarda el nodo de esta subtarea en arbol.json de la raíz.
// Best-effort: un fallo al escribir el árbol no debe frenar la
// recursión — el árbol es para que humanos lo miren, no parte del
// contrato de verificación. Las escrituras concurrentes de las hojas en
// paralelo las serializa muArbol dentro de AgregarNodo.
func (a Agent) registrar(sub task.Task, verde bool, intentos int, motivo string, tk loop.Tokens, modelo string) {
	if a.Root == "" || a.RaizID == "" {
		return
	}
	_ = AgregarNodo(a.Root, a.RaizID, NodoArbol{
		ID: sub.ID, Titulo: sub.Titulo, Padre: a.Task.ID,
		Profundidad: a.Profundidad + 1, Verde: verde, Intentos: intentos,
		Motivo: motivo, Tokens: tk, Modelo: modelo,
	})
}

// modeloInicial resuelve el modelo de una subtarea por su peso: las
// hojas livianas usan el modelo barato de `modelos:` y las pesadas el
// caro, en vez de heredar el de la tarea raíz. Sin mapeo, cae al
// ModeloEjecutor (el del CLI).
func (a Agent) modeloInicial(sub task.Task) string {
	if m := a.Cfg.ModeloPara(sub.Peso); m != "" {
		return m
	}
	return a.ModeloEjecutor
}

// correrSubtarea decide si la subtarea también recursa (si hay
// profundidad disponible) o corre plana con el agente hoja, y la ejecuta.
// modelo es el id a usar en este intento — el de su peso o el escalado.
func (a Agent) correrSubtarea(ctx context.Context, req loop.Request, r room.Room, sub task.Task, modelo string) (loop.Outcome, error, loop.Tokens) {
	hoja := a.Ejecutor
	if a.EjecutorPara != nil {
		hoja = a.EjecutorPara(modelo)
	}
	agente := hoja
	if sub.Recursivo && a.Profundidad+1 < a.Cfg.RecursionMax {
		agente = Agent{
			Cfg: a.Cfg, Constitucion: a.Constitucion,
			Planificador: a.Planificador,
			Ejecutor:     hoja, EjecutorPara: a.EjecutorPara, ModeloEjecutor: modelo,
			Task: sub, Profundidad: a.Profundidad + 1,
			Root: a.Root, RaizID: a.RaizID,
			Presupuesto: a.Presupuesto, Ventanas: a.Ventanas,
		}
	}
	root := rootPara(a, req)
	var agenteTimeout, pruebaTimeout time.Duration
	if a.Cfg.TimeoutAgente > 0 {
		agenteTimeout = time.Duration(a.Cfg.TimeoutAgente) * time.Second
	}
	if a.Cfg.TimeoutPruebas > 0 {
		pruebaTimeout = time.Duration(a.Cfg.TimeoutPruebas) * time.Second
	}
	outcome, err := loop.Run(ctx, loop.Options{
		Agent:          agente,
		Root:           root,
		Room:           r,
		Task:           sub,
		Model:          modelo,
		Base:           "HEAD",
		Constitucion:   a.Constitucion,
		Interfaces:     sub.Usa,
		PatronesPrueba: a.Cfg.PatronesPrueba,
		AgentTimeout:   agenteTimeout,
		PruebaTimeout:  pruebaTimeout,
		Presupuesto:    a.Presupuesto,
		Ventanas:       a.Ventanas,
		Proveedor:      hoja.Name(),
	})
	if err != nil {
		return loop.Outcome{}, err, loop.Tokens{}
	}
	tk, _ := ultimoTokens(root, sub.ID)
	return outcome, nil, tk
}

// rootPara decide dónde viven latidos, attempts y logs de las subtareas:
// la raíz del proyecto si el Agent la conoce (producción), o el cuarto
// del padre como hacía antes (fixtures de prueba que no la pasan).
func rootPara(a Agent, req loop.Request) string {
	if a.Root != "" {
		return a.Root
	}
	return req.RoomPath
}

// ultimoTokens suma los tokens de todos los intentos registrados de una
// subtarea, para que el gasto de la recursión se vea reflejado en el
// intento del padre — sin esto, attempts.jsonl del padre subestima el
// costo real de la tarea.
func ultimoTokens(root, id string) (loop.Tokens, error) {
	attempts, err := loop.ReadAttempts(root, id)
	if err != nil {
		return loop.Tokens{}, err
	}
	var tk loop.Tokens
	for _, at := range attempts {
		tk.Entrada += at.Tokens.Entrada
		tk.Salida += at.Tokens.Salida
	}
	return tk, nil
}

// huboTrabajo reporta si la subtarea llegó a tocar archivos en algún
// intento. Es la señal que separa "el código falló" (vale escalar) de
// "el modelo ni arrancó" (escalar no cambia nada).
func huboTrabajo(root, id string) bool {
	if root == "" {
		return false
	}
	attempts, err := loop.ReadAttempts(root, id)
	if err != nil {
		return false
	}
	for _, at := range attempts {
		if len(at.ArchivosTocados) > 0 {
			return true
		}
	}
	return false
}

// supervisar consulta al orquestador (modelo grande) qué hacer con una
// subtarea roja. Solo devuelve ok=true cuando el dictamen es
// "replanificar" y el contrato nuevo es válido; cualquier otra salida
// (omitir, detener, texto ilegible) se traduce en "sigue roja" y el
// listo_cuando del padre decide al final — el supervisor informa, no
// bloquea, y nunca abre una puerta a loops infinitos.
func (a Agent) supervisar(ctx context.Context, sub task.Task, motivo string) (task.Task, bool) {
	if a.Planificador == nil {
		return task.Task{}, false
	}
	texto, err := a.Planificador.Generar(ctx, a.promptSupervisor(sub, motivo))
	if err != nil {
		return task.Task{}, false
	}
	d, err := parseDictamen(texto)
	if err != nil || d.Decision != "replanificar" || d.Contrato == nil {
		return task.Task{}, false
	}
	nuevo, err := a.replanDesdeContrato(sub, *d.Contrato)
	if err != nil {
		return task.Task{}, false
	}
	return nuevo, true
}

// dictamen es la respuesta del orquestador ante una hoja roja.
type dictamen struct {
	Decision string         `json:"decision"`
	Motivo   string         `json:"motivo"`
	Contrato *plan.Borrador `json:"contrato"`
}

// parseDictamen tolera vallas markdown y texto alrededor, como plan.Parse.
func parseDictamen(texto string) (dictamen, error) {
	t := strings.TrimSpace(texto)
	t = strings.TrimPrefix(t, "```json")
	t = strings.TrimPrefix(t, "```")
	t = strings.TrimSuffix(t, "```")
	t = strings.TrimSpace(t)
	var d dictamen
	if err := json.Unmarshal(idioma.NormalizarClaves([]byte(t), map[string]string{"reason": "motivo", "contract": "contrato"}), &d); err != nil {
		return d, err
	}
	if es, ok := map[string]string{"replan": "replanificar", "skip": "omitir", "stop": "detener"}[d.Decision]; ok {
		d.Decision = es
	}
	if d.Decision == "" {
		return d, errors.New(idioma.T("el orquestador no devolvió una decisión"))
	}
	return d, nil
}

// replanDesdeContrato reescribe el contrato de una subtarea roja con lo
// que devolvió el orquestador. Conserva el id (es el mismo slot de la
// descomposición) y el límite de intentos; el alcance nuevo queda
// restringido al del padre, nunca puede inventarse rutas afuera.
func (a Agent) replanDesdeContrato(sub task.Task, b plan.Borrador) (task.Task, error) {
	if strings.TrimSpace(b.Titulo) == "" || strings.TrimSpace(b.ListoCuando) == "" {
		return task.Task{}, errors.New(idioma.T("el orquestador devolvió un contrato sin titulo ni listo_cuando"))
	}
	if len(b.TocarSolo) == 0 {
		b.TocarSolo = sub.TocarSolo
	}
	nuevo := sub
	nuevo.Titulo = b.Titulo
	nuevo.Porque = b.Porque
	nuevo.ListoCuando = b.ListoCuando
	nuevo.TocarSolo = b.TocarSolo
	nuevo.NoTocar = mergeLists(a.Task.NoTocar, b.NoTocar)
	nuevo.Expone = b.Expone
	nuevo.Usa = b.Usa
	nuevo.Riesgos = joinTexto(a.Task.Riesgos, b.Riesgos)
	nuevo.Peso = b.Peso
	if nuevo.Peso == "" {
		nuevo.Peso = "liviana"
	}
	nuevo.Agente = b.Agente
	nuevo.Notas = joinTexto(sub.Notas, b.Como)
	nuevo.DependeDe = nil
	if errs := nuevo.Validate(); len(errs) > 0 {
		return task.Task{}, errs[0]
	}
	return nuevo, nil
}

// promptSupervisor arma el brief compacto que recibe el orquestador:
// el contrato de la hoja roja, su fallo y lo que el padre espera al
// final. Deliberadamente corto — se gasta solo cuando algo ya falló.
func (a Agent) promptSupervisor(sub task.Task, motivo string) string {
	var b strings.Builder
	b.WriteString("You are the supervisor of a team of coding agents. A subtask failed after exhausting its attempts and its escalation model.\n\n")
	fmt.Fprintf(&b, "Parent task %s: %s\n", a.Task.ID, a.Task.Titulo)
	fmt.Fprintf(&b, "The parent will be done when: %s\n", a.Task.ListoCuando)
	fmt.Fprintf(&b, "\nSubtask %s: %s\n", sub.ID, sub.Titulo)
	if sub.Porque != "" {
		fmt.Fprintf(&b, "Why: %s\n", sub.Porque)
	}
	fmt.Fprintf(&b, "Done when: %s\n", sub.ListoCuando)
	if len(sub.TocarSolo) > 0 {
		fmt.Fprintf(&b, "Scope: %s\n", strings.Join(sub.TocarSolo, ", "))
	}
	fmt.Fprintf(&b, "It failed: %s\n", recortar(motivo, 500))
	b.WriteString("\nDecide what to do. Return ONLY JSON, no text around it, one of these three:\n")
	b.WriteString("{\"decision\":\"replan\",\"reason\":\"...\",\"contract\":{\"title\":\"...\",\"done_when\":\"a command that FAILS TODAY\",\"only_touch\":[\"...\"],\"why\":\"...\",\"how\":\"approach\"}}\n")
	b.WriteString("{\"decision\":\"skip\",\"reason\":\"...\"}  · the parent can be done without this subtask\n")
	b.WriteString("{\"decision\":\"stop\",\"reason\":\"...\"}  · without this subtask the parent cannot go green\n")
	b.WriteString("Rules: replan only if the new done_when stays within the parent's scope and fails today; skip only if the subtask is optional for the parent's done_when.")
	return b.String()
}

// listoPadreVerde corre el listo_cuando del padre en su cuarto. Es la
// última palabra cuando quedan hojas rojas: si pasó, las hojas rojas eran
// opcionales y el árbol se da por bueno.
func listoPadreVerde(ctx context.Context, dir, cmd string, timeoutSeg int) bool {
	if strings.TrimSpace(cmd) == "" {
		return false
	}
	to := 5 * time.Minute
	if timeoutSeg > 0 {
		to = time.Duration(timeoutSeg) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()

	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd", "/c", task.SinTerminal(cmd))
	} else {
		c = exec.CommandContext(ctx, "sh", "-c", task.SinTerminal(cmd))
	}
	c.Dir = dir
	var salida strings.Builder
	c.Stdout = &salida
	c.Stderr = &salida
	// un 0 sin pruebas ejecutadas no vuelve verde al padre (loop.SinPruebas)
	return c.Run() == nil && !loop.SinPruebas(salida.String())
}

// recortar deja el motivo del fallo en una sola línea acotada.
func recortar(s string, max int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

// descomponer pide al planificador que parta la tarea en subtareas
// reales, reusando el mismo parser que `loom plan` — la descomposición
// no es más que un plan cuyo alcance ya viene acotado al `tocar_solo` de
// la tarea padre.
func (a Agent) descomponer(ctx context.Context) ([]plan.Borrador, error) {
	texto, err := a.Planificador.Generar(ctx, a.promptDescomposicion())
	if err != nil {
		return nil, err
	}
	borradores, err := plan.Parse(texto)
	if err != nil {
		return nil, err
	}
	restringirAlcance(borradores, a.Task.TocarSolo)
	for i, b := range borradores {
		if len(b.TocarSolo) == 0 {
			return nil, fmt.Errorf(idioma.T("subtarea %d (%q) quedó sin tocar_solo dentro del alcance de %s"), i+1, b.Titulo, a.Task.ID)
		}
	}
	return borradores, nil
}

// promptDescomposicion es la versión compacta del prompt de plan: lleva
// el contrato del padre (por qué, para qué, cómo se sabe que está,
// riesgos, firmas congeladas) y exige el campo "como" en cada subtarea,
// que es la instrucción que el orquestador le deja al agente chico. Más
// corto que plan.Prompt a propósito: la descomposición corre dentro del
// bucle y cada token cuenta.
//
// Le dice al modelo que las subtareas corren en paralelo cuando pueden:
// las independientes van juntas y una que necesita ver el código de otra
// lo declara por índice en "depende_de".
func (a Agent) promptDescomposicion() string {
	t := a.Task
	limite := t.LimiteSubtareas
	if limite < 1 {
		limite = task.DefaultLimiteSubtareas
	}
	var b strings.Builder
	b.WriteString("You are loom's planner. Split this task into small, verifiable subtasks.\n\n")
	fmt.Fprintf(&b, "Task %s: %s\n", t.ID, t.Titulo)
	if t.Porque != "" {
		fmt.Fprintf(&b, "Why it matters: %s\n", t.Porque)
	}
	if t.Notas != "" {
		fmt.Fprintf(&b, "Parent design and instructions:\n%s\n\n", t.Notas)
	}
	fmt.Fprintf(&b, "The task will be done when: %s (code decides, not a model)\n", t.ListoCuando)
	if t.Riesgos != "" {
		fmt.Fprintf(&b, "Parent risks: %s\n", t.Riesgos)
	}
	if len(t.Expone) > 0 {
		fmt.Fprintf(&b, "The parent must end up exposing: %s\n", strings.Join(t.Expone, "; "))
	}
	if len(t.Usa) > 0 {
		fmt.Fprintf(&b, "The parent consumes from other tasks: %s\n", strings.Join(t.Usa, "; "))
	}
	if len(t.NoTocar) > 0 {
		fmt.Fprintf(&b, "Nadie toca: %s\n", strings.Join(t.NoTocar, ", "))
	}
	fmt.Fprintf(&b, "You may declare \"only_touch\" ONLY within these paths (no others): %s. At most %d subtasks.\n\n",
		strings.Join(t.TocarSolo, ", "), limite)
	b.WriteString("Subtasks run IN PARALLEL when they can, each with a cheap model. They are numbered in order: the first is 1, the second 2, etc. A subtask that needs to see another's code must declare \"depends_on\": [n] with that other's number; without depends_on it is assumed independent and runs in parallel.\n\n")
	b.WriteString("Return ONLY a JSON array, no text around it. Each subtask:\n")
	b.WriteString("- \"title\": short phrase\n")
	b.WriteString("- \"why\": why it matters (one sentence)\n")
	b.WriteString("- \"done_when\": executable command that FAILS TODAY and passes when that subtask is done. If it points to a test file, that file must also be in only_touch.\n")
	b.WriteString("- \"only_touch\": array of globs within the parent's scope; no overlap with other subtasks running in parallel\n")
	b.WriteString("- \"depends_on\": array of subtask numbers that must be green first (e.g. [1]); empty if it depends on none\n")
	b.WriteString("- \"exposes\" / \"uses\": signatures this subtask produces/consumes, if any\n")
	b.WriteString("- \"how\": concrete instructions with files, steps, inputs, outputs, errors and test cases; keep the parent's architecture decisions (mandatory, not limited to one line)\n")
	b.WriteString("- \"risks\": or \"\"\n")
	b.WriteString("- \"weight\": \"light\", \"medium\" or \"heavy\" by complexity (small leaves go to \"light\")\n\n")
	b.WriteString("The set of subtasks must achieve the parent's done_when. Split by layers or files, not by temporal steps: each subtask leaves something green on its own. If a piece is small and well defined, prefer \"light\" and a clear \"how\" — the cheap model only needs to follow precise instructions.\n")
	return b.String()
}

// restringirAlcance recorta de cada borrador los globs que se salen del
// alcance heredado del padre. ponytail: contención por prefijo de
// directorio, no intersección completa de globs — alcanza para el caso
// real (subrutas de tocar_solo) sin escribir un motor de globs nuevo.
func restringirAlcance(bs []plan.Borrador, permitidos []string) {
	if len(permitidos) == 0 {
		return // padre sin alcance declarado: nada que contener
	}
	for i := range bs {
		var dentro []string
		for _, g := range bs[i].TocarSolo {
			if dentroDeAlcance(g, permitidos) {
				dentro = append(dentro, g)
			}
		}
		bs[i].TocarSolo = dentro
	}
}

func dentroDeAlcance(glob string, permitidos []string) bool {
	for _, p := range permitidos {
		if glob == p || strings.HasPrefix(glob, prefijo(p)) {
			return true
		}
	}
	return false
}

// prefijo devuelve la parte literal de un glob antes del primer
// comodín — el directorio que de verdad delimita, "internal/foo/**" →
// "internal/foo/".
func prefijo(glob string) string {
	if i := strings.IndexAny(glob, "*?"); i >= 0 {
		return glob[:i]
	}
	return glob
}

// dependenciaSibling traduce una dependencia declarada por índice ("1")
// al id real de la subtarea hermana ("T-100001"); un id completo pasa
// tal cual.
func dependenciaSibling(padreID, d string) string {
	if n, err := strconv.Atoi(strings.TrimSpace(d)); err == nil && n >= 1 {
		return fmt.Sprintf("%s%03d", padreID, n)
	}
	return d
}

// tareaDesdeBorrador arma el contrato completo de una subtarea a partir
// de lo que propuso el planificador. El id es determinístico y siempre
// válido (`^T-\d{3,}$`): concatena el número de la tarea padre con el
// índice, así que no colisiona ni necesita reservar nada en
// .loom/tasks/ — estas subtareas son efímeras, nunca se guardan en
// disco.
//
// El contexto del padre se propaga a la subtarea: no_tocar y riesgos se
// heredan, limite_lineas se recorta al presupuesto del padre, el enfoque
// ("como") entra como nota, y las dependencias por índice se traducen a
// ids de hermanas. Sin peso declarado, la hoja va a "liviana": el modelo
// barato es el que corresponde a una subtarea chica con instrucciones
// precisas.
func (a Agent) tareaDesdeBorrador(b plan.Borrador, indice int) (task.Task, error) {
	limiteLineas := a.Task.LimiteLineas
	if limiteLineas < 1 {
		limiteLineas = task.DefaultLimiteLineas
	}
	if b.LimiteLineas > 0 {
		limiteLineas = plan.AcotarLimiteLineas(b.LimiteLineas, limiteLineas)
	}
	peso := b.Peso
	if peso == "" {
		peso = "liviana"
	}
	deps := make([]string, 0, len(b.DependeDe))
	for _, d := range b.DependeDe {
		deps = append(deps, dependenciaSibling(a.Task.ID, d))
	}
	sub := task.Task{
		Version:        task.Version,
		ID:             fmt.Sprintf("%s%03d", a.Task.ID, indice),
		Titulo:         b.Titulo,
		Porque:         b.Porque,
		ListoCuando:    b.ListoCuando,
		TocarSolo:      b.TocarSolo,
		NoTocar:        mergeLists(a.Task.NoTocar, b.NoTocar),
		DependeDe:      deps,
		Expone:         b.Expone,
		Usa:            b.Usa,
		Riesgos:        joinTexto(a.Task.Riesgos, b.Riesgos),
		Peso:           peso,
		Agente:         b.Agente,
		Skills:         a.Task.Skills, // la descomposición no ve el catálogo: hereda las del padre
		LimiteIntentos: task.DefaultLimiteIntentos,
		LimiteLineas:   limiteLineas,
		Notas:          joinTexto(a.Task.Notas, b.Como),
	}
	if errs := sub.Validate(); len(errs) > 0 {
		return task.Task{}, errs[0]
	}
	return sub, nil
}

// mergeLists une dos listas de rutas sin duplicados ni vacíos.
func mergeLists(base, extra []string) []string {
	visto := map[string]bool{}
	var out []string
	for _, l := range append(append([]string{}, base...), extra...) {
		if l == "" || visto[l] {
			continue
		}
		visto[l] = true
		out = append(out, l)
	}
	return out
}

// joinTexto une las partes no vacías con " · ", para heredar el contexto
// del padre sin pisar el de la subtarea.
func joinTexto(partes ...string) string {
	var out []string
	for _, p := range partes {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// integrar mergea la rama de una subtarea verde a la rama actual del
// cuarto padre (no a una rama de integración aparte: acá no hay oleada
// paralela que encadenar, es una sola rama que va acumulando).
func integrar(roomPath, rama, id string) error {
	cmd := exec.Command("git", "-c", "user.name=loom", "-c", "user.email=loom@local",
		"merge", "--no-ff", "-m", "loom: integra "+id, rama)
	cmd.Dir = roomPath
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	mergeAbort := exec.Command("git", "merge", "--abort")
	mergeAbort.Dir = roomPath
	_, _ = mergeAbort.CombinedOutput()
	return fmt.Errorf("%s", tail(string(out)))
}

func tail(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.Join(lines, " · ")
}
