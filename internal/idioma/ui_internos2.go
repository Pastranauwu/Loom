package idioma

func init() {
	for es, e := range internos2 {
		textos[es] = e
	}
}

var internos2 = map[string]string{
	"comando no encontrado: %s · instálalo o corrige listo_cuando":                                                                                                  "command not found: %s · install it or fix done_when",
	"listo_cuando tardó más de %s · acota el comando":                                                                                                               "done_when took more than %s · narrow the command",
	"no se pudo ejecutar listo_cuando: %v":                                                                                                                          "could not run done_when: %v",
	"tocar_solo se cruza con %s en %s · ajusta los alcances":                                                                                                        "only_touch overlaps %s in %s · adjust the scopes",
	"tocar_solo incluye la zona prohibida %s · quita %s o ajusta el alcance":                                                                                        "only_touch includes the forbidden zone %s · remove %s or adjust the scope",
	"tocar_solo incluye rutas de prueba (%s casa con %s) · las pruebas no las escribe quien implementa":                                                             "only_touch includes test paths (%s matches %s) · whoever implements does not write the tests",
	"agente desconocido: %s · decláralo en config.yml o usa uno existente (backend, frontend, architect, tester, refactor, ejecutor)":                               "unknown agent: %s · declare it in config.yml or use an existing one (backend, frontend, architect, tester, refactor, executor)",
	"tope de %d intentos agotado · edita %s (listo_cuando, tocar_solo o notas) y reintenta":                                                                         "cap of %d attempts exhausted · edit %s (done_when, only_touch or notes) and retry",
	"listo_cuando pasó sin ejecutar ninguna prueba · %s no tiene suite que lo juzgue · sella una con loom task seal %s o apunta listo_cuando a pruebas que existan": "done_when passed without running any test · %s has no suite to judge it · seal one with loom task seal %s or point done_when at tests that exist",
	"el agente no pudo ejecutarse (%s) · revisa el modelo y la key con loom doctor":                                                                                 "the agent could not run (%s) · check the model and the key with loom doctor",
	"sin progreso: el agente solo tocó archivos fuera de tocar_solo (%s) y se revirtieron · agrégalos a tocar_solo si la tarea los necesita":                        "no progress: the agent only touched files outside only_touch (%s) and they were reverted · add them to only_touch if the task needs them",
	" · si arregla algo que ya estaba roto en la base, su cambio quedó en %s: aplícalo en la base con git apply, haz commit y reintenta":                            " · if it fixes something that was already broken on the base, its change was left in %s: apply it on the base with git apply, commit and retry",
	"agotó %d intentos · falla: %s":                               "ran out of %d attempts · fails: %s",
	"%s ↔ %s · no se pudo comprobar la fusión: %s":                "%s ↔ %s · could not check the merge: %s",
	"%s ↔ %s · verdes por separado, pero al fusionarlas falla %s": "%s ↔ %s · green separately, but when merged %s fails",
	"%s ↔ %s · %s":                          "%s ↔ %s · %s",
	"instalando dependencias de %s en %s":   "installing dependencies of %s in %s",
	"%d commits, uno por tarea":             "%d commits, one per task",
	"%d pantallas en %s":                    "%d screens in %s",
	"las pruebas tardaron más de %s":        "the tests took more than %s",
	"%d fallos que la base no tenía: %s · ": "%d failures the base did not have: %s · ",
	"%s · la suite ya fallaba en %s con esos mismos %d fallos y el conjunto no agrega ninguno": "%s · the suite already failed on %s with those same %d failures and the set adds none",
	"%d líneas de código · %d líneas de prueba · %d archivos · sin límite de líneas":           "%d lines of code · %d lines of tests · %d files · no line limit",
	"%d líneas de %d · %d archivos": "%d lines of %d · %d files",
	" · %d líneas de prueba aparte": " · %d lines of tests apart",
	"presupuesto excedido · %s · reduce el cambio, o sube limite_lineas a %d en .loom/tasks/%s.md":         "budget exceeded · %s · shrink the change, or raise max_lines to %d in .loom/tasks/%s.md",
	"suite oculta superada · brecha=%.1f%%":                                                                "hidden suite passed · gap=%.1f%%",
	"la aceptación `%s` nombra %s, que no existe y ninguna tarea escribe":                                  "the acceptance `%s` names %s, which does not exist and no task writes it",
	"la aceptación `%s` nombra %s, que no existe: la prueba está en %s · corrige la ruta en la aceptación": "the acceptance `%s` names %s, which does not exist: the test is in %s · fix the path in the acceptance",
	"%s depende de %s, que no existe":                                                                      "%s depends on %s, which does not exist",
	"%s usa %q pero el plan expone %q":                                                                     "%s uses %q but the plan exposes %q",
	"%s usa %q y ninguna tarea lo expone":                                                                  "%s uses %q and no task exposes it",
	"%s y %s pueden escribir la misma zona (%s / %s)":                                                      "%s and %s may write the same zone (%s / %s)",
	"contrato versión %d, binario soporta %d · actualiza loom":                                             "contract version %d, binary supports %d · update loom",
	"campo desconocido ignorado: %s · si es de una versión más nueva, actualiza loom":                      "unknown field ignored: %s · if it is from a newer version, update loom",
	"se revisa sobre lo integrado: ":                                                                       "checked on the integrated set: ",
	"lo que marcó su esclusa ya no está en lo integrado":                                                   "what its gate flagged is no longer in the integrated set",
	"· rama huérfana %s · sin contrato, se borra":                                                          "· orphan branch %s · no contract, deleted",
	"· verdes sin entregar de la versión anterior: %s · run las suma a la integración y ship --all las entrega con lo nuevo (o cierra antes la versión con ship --all y archive)": "· green tasks not shipped from the previous version: %s · run adds them to integration and ship --all ships them with the new ones (or close the version first with ship --all and archive)",
	"%w · un valor con ': ' va entre comillas (feature: \"Panel: exámenes\")":                                                                                                     "%w · a value containing ': ' must be quoted (feature: \"Panel: exams\")",
	"config.yml: línea %d · tope_tokens_tarea inválido: %s · entero, mínimo 0 (0 = sin tope)":                                                                                     "config.yml: line %d · invalid task_token_cap: %s · integer, minimum 0 (0 = no cap)",
	"%s pasó su tope de %s tokens (tope_tokens_tarea) · el contrato suele estar mal: revisa listo_cuando y tocar_solo antes de reintentar":                                        "%s went over its %s token cap (task_token_cap) · the contract is usually wrong: check done_when and only_touch before retrying",
}
