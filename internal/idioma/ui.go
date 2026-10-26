package idioma

// T translates a user-facing text written in Spanish into English unless the
// language is Spanish. An unknown text comes back unchanged, so a message
// nobody translated yet still reads in Spanish instead of breaking.
func T(es string) string {
	if Es() {
		return es
	}
	if en, ok := textos[es]; ok {
		return en
	}
	return es
}

// textos maps each Spanish UI text to its English version. Keep the printf
// verbs in the same order.
var textos = map[string]string{
	"tú diseñas el patrón · loom teje el código": "you design the pattern · loom weaves the code",
	"✓ idioma: %s": "✓ language: %s",
	"← → navegar · enter siguiente · q salir": "← → navigate · enter next · q quit",
	"← atrás · enter o q para salir":          "← back · enter or q to exit",
	"tutorial":                                "tutorial",
	"· nuevo en loom · corre loom tutorial para un recorrido guiado": "· new to loom · run loom tutorial for a guided tour",
	// run view
	"CORRIDA": "RUN", "pendiente": "pending", "pendientes": "pending", "verdes": "green",
	"verde en ": "green in ", "sin saldo · queda pendiente": "out of credit · stays pending",
	"detenida": "stopped", "intento %d": "attempt %d", "1 intento": "1 attempt", " intentos": " attempts",
	"ATASCO · ": "STUCK · ", " sin señal": " no signal",
	// attempt phases
	"agente": "agent", "examen": "exam", "verificando": "verifying", "revision": "review", "trabajando": "working",
	// board
	"LISTO PARA ENTREGAR": "READY TO SHIP", "EN CURSO": "RUNNING", "DETENIDO": "STOPPED", "PENDIENTE": "PENDING",
	"INTERRUMPIDA · sin señal hace ": "INTERRUPTED · no signal for ", " · run --reintentar": " · run --retry",
	"presupuesto ": "budget ", " tareas": " tasks", " líneas más arriba": " lines above", " líneas más abajo": " lines below",
	"sin tareas · empieza con loom plan \"lo que necesitas\"":                 "no tasks · start with loom plan \"what you need\"",
	"j/k · / filtra · d detalle · s entrega · S todas · r reintenta · q sale": "j/k · / filter · d detail · s ship · S all · r retry · q quit",
	// exit gate
	"ESCLUSA DE SALIDA · ": "EXIT GATE · ",
	"historial":            "history", "ruido": "noise", "secretos": "secrets", "presupuesto": "budget",
	"dependencias": "dependencies", "suite_oculta": "hidden suite",
	"hist": "hist", "secr": "secrets", "presu": "budget", "iface": "iface", "deps": "deps", "bisec": "bisect", "oculta": "hidden", "hand": "handoff",
	"dry-run · sin PR": "dry-run · no PR", "entregable · --dry-run, sin PR": "deliverable · --dry-run, no PR",
	"entregable · %d tareas en la rama %s · --dry-run, sin PR": "deliverable · %d tasks on branch %s · --dry-run, no PR",
	"sin ruido": "no noise", "expone lo prometido": "exposes what was promised",
	// lists and spinner
	"j/k mueve · enter elige · q cancela":                                          "j/k move · enter pick · q cancel",
	"j/k mueve · espacio marca · a todas · n ninguna · enter confirma · q cancela": "j/k move · space mark · a all · n none · enter confirm · q cancel",
	" más arriba": " above", " más abajo": " below", "interrumpido": "interrupted",
	// task detail
	"INTENTOS · ": "ATTEMPTS · ", "  la tarea todavía no corre": "  the task has not run yet", " · salida ": " · exit ",
	"    archivos": "    files", "    revertido": "    reverted",
	// commands
	"✓ dna activo: %s": "✓ active dna: %s", "· sin dna · loom dna add <nombre>": "· no dna · loom dna add <name>",
	"cancelado · no se escribió nada": "cancelled · nothing was written", "descartado · no se escribió nada": "discarded · nothing was written",
	"¿escribir %s? [s/n]": "write %s? [y/n]",
	"✓ spec escrito en %s · siguiente: loom apply --dry-run · loom up": "✓ spec written to %s · next: loom apply --dry-run · loom up",
	"sketch · buscando lo que no pensaste":                             "sketch · finding what you did not think of", "sketch · armando el spec": "sketch · writing the spec",
	"¿Qué quieres construir?": "What do you want to build?", "una línea · enter envía · esc cancela": "one line · enter sends · esc cancels",
	"enter sin escribir = que decida loom · esc cancela": "enter with no text = let loom decide · esc cancels",
	"heal · buscando qué le faltaba al spec":             "heal · finding what the spec was missing",
	"causa      %s": "cause      %s", "regla      %s": "rule       %s", "criterio   %s": "criterion  %s", "reproduce  %s": "reproduces %s", "tocar      %s": "touches    %s",
}
