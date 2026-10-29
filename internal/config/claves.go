package config

import (
	"regexp"
	"strings"
)

// config.yml is written with English keys; the Spanish ones (the names
// before loom was English-first) are still read. Parse works on the Spanish
// names, so the file is translated on the way in and back on the way out.

var clavesTop = [][2]string{ // Spanish, English
	{"pruebas", "tests"}, {"entrega", "delivery"}, {"zonas_prohibidas", "forbidden_zones"},
	{"pantallas_levantar", "screens_start"}, {"pantallas_semilla", "screens_seed"}, {"pantallas_script", "screens_script"},
	{"pantallas_url", "screens_url"}, {"pantallas_rutas", "screens_routes"}, {"pantallas_muestra", "screens_sample"}, {"pantallas_max", "screens_max"}, {"escalar_tras", "escalate_after"}, {"patrones_prueba", "test_patterns"},
	{"timeout_esclusa", "gate_timeout"}, {"arquitecto_economico", "economy_architect"}, {"agentes_pagados", "paid_agents"},
	{"tope_arquitecto_usd", "architect_cap_usd"}, {"timeout_agente", "agent_timeout"}, {"timeout_pruebas", "tests_timeout"},
	{"recursion_max", "max_recursion"}, {"subagentes", "subagents"}, {"presupuesto_tokens", "token_budget"},
	{"estrategia", "strategy"}, {"reglas_import", "import_rules"}, {"presupuesto", "budget"}, {"modelos", "models"},
	{"proveedores", "providers"}, {"agentes", "agents"}, {"aislamiento", "sandbox"},
}

// children of a block, by the block's Spanish name
var clavesHijas = map[string][][2]string{
	"modelos":     {{"liviana", "light"}, {"media", "medium"}, {"pesada", "heavy"}},
	"proveedores": {{"planificador", "planner"}, {"ejecutor", "executor"}, {"revisor", "reviewer"}, {"examinador", "examiner"}},
}

// names inside the inline maps of a block
var clavesInline = map[string][][2]string{
	"presupuesto": {{"semanal", "weekly"}, {"mensual", "monthly"}},
	"proveedores": {{"modelo", "model"}},
}

var reClave = regexp.MustCompile(`^(\s*)([A-Za-z_][A-Za-z_0-9-]*)(\s*:)`)

// aIngles rewrites a config written with Spanish keys to English keys.
func aIngles(texto string) string { return traducirClaves(texto, false) }

// aEspanol rewrites English keys back to the Spanish names Parse reads.
func aEspanol(texto string) string { return traducirClaves(texto, true) }

func traducirClaves(texto string, haciaEspanol bool) string {
	par := func(p [2]string) (de, a string) {
		if haciaEspanol {
			return p[1], p[0]
		}
		return p[0], p[1]
	}
	buscar := func(tabla [][2]string, clave string) (string, bool) {
		for _, p := range tabla {
			if de, a := par(p); de == clave {
				return a, true
			}
		}
		return "", false
	}
	var bloque string // Spanish name of the current top-level block
	lineas := strings.Split(texto, "\n")
	for i, l := range lineas {
		m := reClave.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		sangria, clave := m[1], m[2]
		resto := l[len(m[0]):]
		if sangria == "" {
			nueva := clave
			if t, ok := buscar(clavesTop, clave); ok {
				nueva = t
			}
			// the block name is always kept in Spanish to pick the tables
			if haciaEspanol {
				bloque = nueva
			} else {
				bloque = clave
			}
			lineas[i] = nueva + m[3] + resto
		} else if hijas, ok := clavesHijas[bloque]; ok {
			if t, ok := buscar(hijas, clave); ok {
				lineas[i] = sangria + t + m[3] + resto
			}
		}
		if inl, ok := clavesInline[bloque]; ok && strings.Contains(lineas[i], "{") {
			for _, p := range inl {
				de, a := par(p)
				lineas[i] = regexp.MustCompile(`\b`+regexp.QuoteMeta(de)+`(\s*:)`).ReplaceAllString(lineas[i], a+"$1")
			}
		}
	}
	return strings.Join(lineas, "\n")
}
