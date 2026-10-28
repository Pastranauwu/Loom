package main

import (
	"context"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"strconv"
	"strings"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/esqueleto"
	"github.com/Pastranauwu/loom/internal/plan"
	"github.com/Pastranauwu/loom/internal/spec"
	"github.com/Pastranauwu/loom/internal/task"
)

// faltaContrato reporta si una tarea del spec rápido no llega a contrato:
// sin listo_cuando no hay "listo", y sin tocar_solo la esclusa de entrada
// no la deja correr junto a otras.
func faltaContrato(t task.Task, total int) bool {
	return strings.TrimSpace(t.ListoCuando) == "" || (total > 1 && len(t.TocarSolo) == 0)
}

func contarSinContrato(ts []task.Task) int {
	n := 0
	for _, t := range ts {
		if faltaContrato(t, len(ts)) {
			n++
		}
	}
	return n
}

// completarSpec deja que el planificador escriba lo que el humano no
// escribió. El spec rápido es una lista de títulos: quién decide QUÉ se
// hace sigue siendo el humano, y el modelo pone el cómo se verifica, qué
// toca y de qué depende. Lo que el humano sí escribió no se pisa.
func completarSpec(root string, s *spec.Spec) error {
	// En el modo Requirements as Code no hay tareas humanas que completar:
	// el planificador produce el IR entero a partir de intención y reglas.
	if len(s.Tasks) == 0 && len(s.Requirements) > 0 {
		return planearRequirements(root, s)
	}
	n := contarSinContrato(s.Tasks)
	if n == 0 {
		return nil
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	ex, err := elegirEjecutor(cfg.Cli)
	if err != nil {
		return err
	}
	modelo := config.ModeloRol(cfg, "planificador")
	pctx, zonas, patrones, err := contextoPlan(root, cfg, "")
	if err != nil {
		return err
	}
	// los ids van antes que el modelo: así puede escribir depende_de con
	// los ids que de verdad van a existir
	if s.Tasks, err = spec.AssignCorrelativeIDs(config.TasksDir(root), s.Tasks); err != nil {
		return err
	}

	var bs []plan.Borrador
	err = esperarPlan(fmt.Sprintf("completando %d tareas · %s", n, modelo), func(avance func(string)) error {
		var err error
		bs, err = plan.Completar(context.Background(), planGuardado{generadorPlan{ex: ex, modelo: modelo, root: root, effort: "medium", avance: avance}, root, ""}, pctx, s.Feature, s.Reglas, s.Tasks)
		return err
	})
	if err != nil {
		return err
	}
	sanearAlcance(bs, zonas, patrones, pctx.Ocupados)
	sanearSkills(bs, pctx.Skills)
	if pctx.PruebasPropias {
		ampliarPruebasPropias(bs)
	}

	ids := make([]string, len(s.Tasks))
	for i, t := range s.Tasks {
		ids[i] = t.ID
	}
	defLineas := task.DefaultLimiteLineas
	if s.Limites.Lineas > 0 {
		defLineas = s.Limites.Lineas
	}
	total := len(s.Tasks)
	for i := range s.Tasks {
		if faltaContrato(s.Tasks[i], total) {
			s.Tasks[i] = completarTarea(s.Tasks[i], bs[i], ids, defLineas, s.Agente)
		}
	}
	out.Line(idioma.T("· el planificador completó %d de %d tareas · revísalas con loom board"), n, total)
	return nil
}

// planearRequirements convierte la intención del spec en el pedido del
// arquitecto: requirements, reglas y aceptación, numerados para que el
// esqueleto pueda citar a qué requisito responde cada módulo.
func planearRequirements(root string, s *spec.Spec) error {
	var pedido strings.Builder
	fmt.Fprintf(&pedido, "Feature: %s\n", s.Feature)
	d := deltaRequirements(reqsDe(s.Previos, s.PreviosIDs), reqsDe(s.Requirements, s.IDs()))
	tareas, _ := task.List(config.TasksDir(root))
	if len(d.hechos) > 0 {
		pedido.WriteString("Ya implementados en un cambio anterior (están en el código y en " + esqueleto.Documento + "; no generes tareas para ellos salvo que lo nuevo los cambie):\n")
		for _, r := range d.hechos {
			fmt.Fprintf(&pedido, "- [%s] %s\n", r.ID, r.Texto)
		}
	}
	if len(d.cambiados) > 0 {
		pedido.WriteString("Cambiaron (mismo id, texto nuevo): ajusta lo que ya existe, no lo dupliques:\n")
		for _, c := range d.cambiados {
			fmt.Fprintf(&pedido, "- [%s] antes: %s\n  ahora: %s\n", c.ahora.ID, c.antes.Texto, c.ahora.Texto)
			pedido.WriteString(cubiertoPor(tareas, c.ahora.ID))
		}
	}
	if len(d.retirados) > 0 {
		// un id sin declarar sale del texto: reescribir la redacción llega
		// aquí y como nuevo. Decir "quítalos" hacía borrar funcionalidad
		// que el humano solo redactó
		pedido.WriteString("Ya no están en el spec (pueden haberse reescrito abajo con otro texto). Quita del código y de " + esqueleto.Documento + " solo lo que ningún requerimiento vigente siga pidiendo:\n")
		for _, r := range d.retirados {
			fmt.Fprintf(&pedido, "- [%s] %s\n", r.ID, r.Texto)
			pedido.WriteString(cubiertoPor(tareas, r.ID))
		}
	}
	if len(d.nuevos) > 0 {
		pedido.WriteString("Requerimientos obligatorios (pon su id en \"cubre\" de cada tarea que los implemente):\n")
		for _, r := range d.nuevos {
			fmt.Fprintf(&pedido, "[%s] %s\n", r.ID, r.Texto)
		}
	} else if len(d.cambiados) == 0 {
		pedido.WriteString("Los requerimientos no cambiaron: cambiaron las reglas, la aceptación o las restricciones. Ajusta solo lo que eso exige.\n")
	}
	if len(s.Previos) > 0 {
		pedido.WriteString(deltaDeclarado(s.Delta))
	} else {
		if len(s.Reglas) > 0 {
			pedido.WriteString("Reglas obligatorias:\n")
			for _, r := range s.Reglas {
				fmt.Fprintf(&pedido, "- %s\n", r)
			}
		}
		if len(s.Acceptance) > 0 {
			pedido.WriteString("Aceptación global (la prueba de integración tiene que cubrirla):\n")
			for _, a := range s.Acceptance {
				fmt.Fprintf(&pedido, "- %s\n", textoAceptacion(a))
			}
		}
		if len(s.Constraints.NoTocar) > 0 {
			fmt.Fprintf(&pedido, "No tocar: %s\n", strings.Join(s.Constraints.NoTocar, ", "))
		}
	}
	return planearEsqueleto(root, s, pedido.String())
}

// req es un requerimiento con su id.
type req struct{ ID, Texto string }

func reqsDe(textos, ids []string) []req {
	ids = spec.Spec{Requirements: textos, RequirementIDs: ids}.IDs()
	out := make([]req, len(textos))
	for i, t := range textos {
		out[i] = req{ID: ids[i], Texto: t}
	}
	return out
}

type cambio struct{ antes, ahora req }

type delta struct {
	nuevos, hechos, retirados []req
	cambiados                 []cambio
}

// deltaRequirements separa el spec actual contra el último planeado, por
// id: nuevos, los que ya estaban igual, los que cambiaron de texto con
// el mismo id (id explícito en el spec) y los que se quitaron. Sin plan
// previo, todo es nuevo.
func deltaRequirements(previos, actuales []req) delta {
	var d delta
	antes := map[string]req{}
	for _, r := range previos {
		antes[r.ID] = r
	}
	ahora := map[string]bool{}
	for _, r := range actuales {
		ahora[r.ID] = true
		p, ok := antes[r.ID]
		switch {
		case !ok:
			d.nuevos = append(d.nuevos, r)
		case p.Texto != r.Texto:
			d.cambiados = append(d.cambiados, cambio{antes: p, ahora: r})
		default:
			d.hechos = append(d.hechos, r)
		}
	}
	for _, r := range previos {
		if !ahora[r.ID] {
			d.retirados = append(d.retirados, r)
		}
	}
	return d
}

// cubiertoPor dice qué tareas implementaron un requerimiento y qué
// archivos tocaron: es por donde el arquitecto empieza a cambiarlo o
// quitarlo, sin adivinar en todo el repo.
func cubiertoPor(tareas []task.Task, id string) string {
	var b strings.Builder
	for _, t := range tareas {
		for _, c := range t.Cubre {
			if c == id {
				fmt.Fprintf(&b, "  lo implementó %s «%s» en %s\n", t.ID, t.Titulo, strings.Join(t.TocarSolo, ", "))
			}
		}
	}
	return b.String()
}

// completarTarea rellena los campos vacíos de t con los del borrador.
func completarTarea(t task.Task, b plan.Borrador, ids []string, defLineas int, agenteSpec string) task.Task {
	if strings.TrimSpace(t.ListoCuando) == "" {
		t.ListoCuando = b.ListoCuando
	}
	if len(t.TocarSolo) == 0 {
		t.TocarSolo = b.TocarSolo
	}
	if len(t.NoTocar) == 0 {
		t.NoTocar = b.NoTocar
	}
	if len(t.DependeDe) == 0 {
		t.DependeDe = dependenciasDelModelo(b.DependeDe, ids, nil)
	}
	if len(t.Expone) == 0 {
		t.Expone = b.Expone
	}
	if len(t.Usa) == 0 {
		t.Usa = b.Usa
	}
	if t.Porque == "" {
		t.Porque = b.Porque
	}
	if t.Riesgos == "" {
		t.Riesgos = b.Riesgos
	}
	if t.Peso == "" {
		t.Peso = b.Peso
	}
	// el agente por defecto del spec es una decisión del humano
	if t.Agente == "" && agenteSpec == "" {
		t.Agente = b.Agente
	}
	if t.Skills == nil {
		t.Skills = b.Skills
	}
	if t.Notas == "" {
		t.Notas = b.Como
	} else if b.Como != "" && b.Como != t.Notas {
		t.Notas += "\n\n" + b.Como
	}
	// El límite del contrato o del spec lo decide el humano.
	if t.LimiteLineas == 0 {
		t.LimiteLineas = plan.AcotarLimiteLineas(b.LimiteLineas, defLineas)
	}
	return t
}

// dependenciasDelModelo acepta tal cual los ids de la lista y los de
// tareas previas y, si el modelo numeró por su cuenta ("T-001" sin estar
// en ninguna), lo lee como posición.
func dependenciasDelModelo(deps, ids []string, previas map[string]bool) []string {
	enLista := make(map[string]bool, len(ids))
	for _, id := range ids {
		enLista[id] = true
	}
	var out []string
	for _, d := range deps {
		// "2" con un plan que arranca en T-002 es el id, no la segunda
		// tarea: el prompt dice desde qué id numera. Leído como posición
		// armaba ciclos (2 → T-003)
		if n, err := strconv.Atoi(strings.TrimSpace(d)); err == nil && enLista[fmt.Sprintf("T-%03d", n)] {
			d = fmt.Sprintf("T-%03d", n)
		}
		if !enLista[d] && !previas[d] {
			d = task.DependenciasPorPosicion([]string{d}, ids)[0]
		}
		out = append(out, d)
	}
	return out
}

func textoAceptacion(a spec.Acceptance) string {
	if a.Command == "" {
		return a.Criterion
	}
	return a.Criterion + " · comando: " + a.Command
}

// deltaDeclarado lista solo lo que cambió en reglas, aceptación y
// restricciones desde el último plan: lo demás ya está construido.
func deltaDeclarado(d spec.Delta) string {
	var b strings.Builder
	for _, g := range []struct{ clase, titulo string }{
		{spec.ClaseRegla, "reglas"},
		{spec.ClaseAceptacion, "aceptación global"},
		{spec.ClaseRestriccion, "no tocar"},
	} {
		x := d.De(g.clase)
		for _, c := range x.Agregado {
			fmt.Fprintf(&b, "Nueva en %s: %s\n", g.titulo, c.Ahora)
		}
		for _, c := range x.Modificado {
			fmt.Fprintf(&b, "Cambió en %s: antes «%s», ahora «%s»\n", g.titulo, c.Antes, c.Ahora)
		}
		for _, c := range x.Eliminado {
			fmt.Fprintf(&b, "Ya no está en %s: %s\n", g.titulo, c.Antes)
		}
	}
	if b.Len() > 0 {
		return "Cambios en lo declarado (solo ajusta los módulos que esto afecta):\n" + b.String()
	}
	return ""
}
