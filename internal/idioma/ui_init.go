package idioma

func init() {
	for es, en := range iniciales {
		textos[es] = en
	}
}

// iniciales holds the English text of init, the pickers and doctor.
var iniciales = map[string]string{
	"Usa los proveedores y modelos configurados en OpenCode":               "Uses the providers and models configured in OpenCode",
	"Usa tu sesión de Claude · Opus planifica, Haiku y Sonnet implementan": "Uses your Claude session · Opus plans, Haiku and Sonnet implement",
	"Usa tu sesión de Codex (ChatGPT) y sus modelos gpt":                   "Uses your Codex (ChatGPT) session and its gpt models",
	"CLI DE AGENTE": "AGENT CLI",
	"↑/↓ mueve · enter elige · esc o ctrl+c cancela": "↑/↓ move · enter pick · esc or ctrl+c cancel",
	"MODELOS POR PESO DE TAREA":                      "MODELS BY TASK WEIGHT",
	"j/k mueve · enter elige · q deja la propuesta":  "j/k move · enter pick · q keep the proposal",
	"usar la propuesta":                              "use the proposal",
	"elegir yo":                                      "pick myself",
	"%d modelos disponibles en el CLI":               "%d models available in the CLI",
	"MODELO PARA TAREAS DE PESO ":                    "MODEL FOR TASKS OF WEIGHT ",
	"j/k mueve · enter elige · q deja ":              "j/k move · enter pick · q keep ",
	// doctor
	"repositorio": "repository", "configuración": "configuration", "ejecutores": "executors", "remoto": "remote", "modelos": "models",
	"git no está instalado": "git is not installed", "sin .loom · corre loom init": "no .loom · run loom init",
	"ninguno instalado · instala opencode o claude":                "none installed · install opencode or claude",
	"ninguna key de proveedor en el entorno":                       "no provider key in the environment",
	"no instalado · loom skills sync no va a poder traer skills":   "not installed · loom skills sync will not be able to fetch skills",
	"catálogo no consultable · se usará el modelo por defecto de ": "catalog not queryable · using the default model of ",
	"sin `modelos:` · se usará el modelo por defecto de ":          "no `models:` · using the default model of ",
}
