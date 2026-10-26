package ship

import (
	"path"
	"regexp"
	"strings"
)

// verificarDNA hace cumplir por código lo que el ADN del proyecto puede
// comprobarse en el diff: zero-deps (ninguna dependencia nueva en los
// manifiestos) y, con hexagonal o functional-core y sin reglas_import propias,
// que el dominio no importe adaptadores ni infraestructura.
func verificarDNA(dna []string, diff string, reglasImport []string) (string, bool) {
	activo := map[string]bool{}
	for _, n := range dna {
		activo[n] = true
	}
	var problemas []string
	if activo["zero-deps"] {
		if nuevas := dependenciasNuevas(diff); len(nuevas) > 0 {
			problemas = append(problemas, "zero-deps: dependencias nuevas · "+strings.Join(nuevas, "; "))
		}
	}
	if (activo["hexagonal"] || activo["functional-core"]) && len(reglasImport) == 0 {
		if detalle, ok := verificarDependencias(diff, reglasImportDeDominio); !ok {
			problemas = append(problemas, "el dominio no importa infraestructura · "+detalle)
		}
	}
	if len(problemas) > 0 {
		return strings.Join(problemas, " · "), false
	}
	return "cumple el dna: " + strings.Join(dna, ", "), true
}

// reglasImportDeDominio es la restricción por defecto del ADN hexagonal:
// los nombres de capa se buscan en las rutas, como en reglas_import.
var reglasImportDeDominio = func() []string {
	var out []string
	for _, borde := range []string{"adapters", "adapter", "infrastructure", "infra"} {
		for _, centro := range []string{"domain", "core"} {
			out = append(out, borde+" → "+centro)
		}
	}
	return out
}()

var (
	reGoMod       = regexp.MustCompile(`^\s*(?:require\s+)?[A-Za-z0-9][\w.\-]*\.[A-Za-z]{2,}[\w./\-~]*\s+v\d`)
	reNpmDep      = regexp.MustCompile(`^\s*"([^"]+)"\s*:\s*"(?:[\^~<>=*\d]|latest|workspace:|file:|git|https?:|npm:)[^"]*",?\s*$`)
	rePipDep      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.\-]*(?:\[[^\]]*\])?\s*(?:[<>=!~;@].*)?$`)
	rePyprojQuote = regexp.MustCompile(`^\s*"[A-Za-z0-9][A-Za-z0-9_.\-]*(?:\[[^\]]*\])?\s*(?:[<>=!~;@][^"]*)?",?\s*$`)
	rePoetryDep   = regexp.MustCompile(`^\s*([A-Za-z0-9][A-Za-z0-9_.\-]*)\s*=\s*(?:"[\^~<>=*\d][^"]*"|\{[^}]*(?:version|git|path)\s*=)`)
	reCargoDep    = regexp.MustCompile(`^\s*([A-Za-z0-9][A-Za-z0-9_\-]*)\s*=\s*(?:"[\^~<>=*\d][^"]*"|\{[^}]*(?:version|git|path)\s*=)`)

	// claves que parecen "nombre = versión" pero son metadatos del paquete
	noEsDependencia = map[string]bool{
		"version": true, "name": true, "edition": true, "license": true, "description": true, "main": true,
		"module": true, "types": true, "node": true, "npm": true, "python": true, "rust-version": true,
		"requires-python": true, "resolver": true, "readme": true, "repository": true, "homepage": true,
	}
)

// dependenciasNuevas lista, por manifiesto, las dependencias que el diff añade.
func dependenciasNuevas(diff string) []string {
	var out []string
	for _, ad := range parseDiffAnadido(diff) {
		base := path.Base(ad.nombre)
		for _, l := range ad.lineas {
			if esDependenciaNueva(base, l) {
				out = append(out, ad.nombre+": "+strings.TrimSpace(l))
			}
		}
	}
	return out
}

func esDependenciaNueva(manifiesto, linea string) bool {
	t := strings.TrimSpace(linea)
	if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "//") {
		return false
	}
	switch {
	case manifiesto == "go.mod":
		return !strings.HasPrefix(t, "replace") && !strings.HasPrefix(t, "exclude") && !strings.HasPrefix(t, "retract") && reGoMod.MatchString(linea)
	case manifiesto == "package.json":
		m := reNpmDep.FindStringSubmatch(linea)
		return m != nil && !noEsDependencia[m[1]]
	case manifiesto == "requirements.txt" || (strings.HasPrefix(manifiesto, "requirements") && strings.HasSuffix(manifiesto, ".txt")):
		return !strings.HasPrefix(t, "-") && rePipDep.MatchString(t)
	case manifiesto == "pyproject.toml":
		if rePyprojQuote.MatchString(linea) {
			return true
		}
		m := rePoetryDep.FindStringSubmatch(linea)
		return m != nil && !noEsDependencia[m[1]]
	case manifiesto == "Cargo.toml":
		m := reCargoDep.FindStringSubmatch(linea)
		return m != nil && !noEsDependencia[m[1]]
	}
	return false
}
