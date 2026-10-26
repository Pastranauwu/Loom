package idioma

func init() {
	for es, en := range redteamTextos {
		textos[es] = en
	}
}

var redteamTextos = map[string]string{
	"di qué atacar · loom redteam T-001 o loom redteam --all":                    "say what to attack · loom redteam T-001 or loom redteam --all",
	"ninguna tarea lista expone algo que atacar":                                 "no ready task exposes anything to attack",
	"· se atacaría %s · %s · %d firmas expuestas · 1 llamada al modelo":          "· would attack %s · %s · %d exposed signatures · 1 model call",
	"redteam · atacando %s":                                                      "redteam · attacking %s",
	"· %s · no se pudo atacar · %v":                                              "· %s · could not be attacked · %v",
	"· %s · redteam omitido · %s":                                                "· %s · redteam skipped · %s",
	"redteam · %s · %d ataques":                                                  "redteam · %s · %d attacks",
	"  · %d ataques descartados por no apoyarse en nada real":                    "  · %d attacks discarded for resting on nothing real",
	"redteam omitido · %v":                                                       "redteam skipped · %v",
	"redteam omitido · ":                                                         "redteam skipped · ",
	"%d ataques · %d confirmados · %d afirmaciones · %d descartados":             "%d attacks · %d confirmed · %d claims · %d discarded",
	"· %s tiene un crash confirmado pero no está lista · arréglala con loom run": "· %s has a confirmed crash but is not ready · fix it with loom run",
	"· %s reabierta con el ataque de %s en su contrato · se reusa su cuarto":     "· %s reopened with the %s attack in its contract · its room is reused",
	"· ningún crash confirmado que reabrir":                                      "· no confirmed crash to reopen",
	"config.yml: línea %d · redteam inválido: %s · usa off, warn o block":        "config.yml: line %d · invalid redteam: %s · use off, warn or block",
	"--redteam acepta warn o block":                                              "--redteam accepts warn or block",
}
