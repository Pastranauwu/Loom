// Package idioma holds the labels loom looks for in model output and
// contracts, in English and Spanish, and the language of human-facing prose.
package idioma

import (
	"encoding/json"
	"strings"
)

var (
	Casos    = []string{"Cases:", "Casos:"}
	Idea     = []string{"Idea:"}
	Obsoleto = []string{"Obsolete:", "Obsoleto:"}
)

// Tiene reports whether s contains any of the labels.
func Tiene(s string, etiquetas []string) bool {
	for _, e := range etiquetas {
		if strings.Contains(s, e) {
			return true
		}
	}
	return false
}

// Cortar splits s after the first label found.
func Cortar(s string, etiquetas []string) (antes, despues string, ok bool) {
	for _, e := range etiquetas {
		if a, d, hay := strings.Cut(s, e); hay {
			return a, d, true
		}
	}
	return s, "", false
}

// Prefijo reports whether s starts with any of the prefixes and returns the one that matched.
func Prefijo(s string, prefijos ...string) (string, bool) {
	for _, p := range prefijos {
		if strings.HasPrefix(s, p) {
			return p, true
		}
	}
	return "", false
}

var lang = "en"

// Set picks the language of human-facing prose: "es" or anything else for English.
func Set(l string) {
	if l == "es" {
		lang = "es"
		return
	}
	lang = "en"
}

// Instruccion is the line appended to prompts whose output a person reads.
func Instruccion() string {
	if lang == "es" {
		return "\nWrite any prose meant for the human (notes, descriptions, reasons, messages) in Spanish. Keep code, identifiers, JSON keys and labels exactly as specified."
	}
	return ""
}

// NormalizarClaves renames the top-level keys of a JSON object from their
// English names to the canonical ones, unless the canonical key is present.
// Models answer in whichever language the prompt used; both must parse.
func NormalizarClaves(data []byte, tabla map[string]string) []byte {
	var m map[string]json.RawMessage
	if json.Unmarshal(data, &m) != nil {
		return data
	}
	for en, es := range tabla {
		if v, ok := m[en]; ok {
			if _, hay := m[es]; !hay {
				m[es] = v
			}
			delete(m, en)
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		return data
	}
	return out
}
