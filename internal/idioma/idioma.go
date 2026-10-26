// Package idioma holds the labels loom looks for in model output and
// contracts, in English and Spanish, and the language of human-facing prose.
package idioma

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// fromEnv lets LOOM_LANGUAGE (en or es) win over config.yml.
func fromEnv(def string) string {
	if e := os.Getenv("LOOM_LANGUAGE"); e == "es" || e == "en" {
		return e
	}
	return def
}

// Set picks the language of human-facing prose: "es" or anything else for English.
// Order: LOOM_LANGUAGE, then l (the project's config), then the user's own
// default (~/.loom/language), then English.
func Set(l string) {
	l = fromEnv(l)
	if l != "es" && l != "en" {
		l = Preferido()
	}
	if l == "es" {
		lang = "es"
		return
	}
	lang = "en"
}

// Actual is the language in use: "en" or "es".
func Actual() string {
	if Es() {
		return "es"
	}
	return "en"
}

func archivoPreferido() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".loom", "language")
}

// Preferido is the language the user chose for every project ("" if none).
func Preferido() string {
	p := archivoPreferido()
	if p == "" {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	if l := strings.TrimSpace(string(b)); l == "en" || l == "es" {
		return l
	}
	return ""
}

// GuardarPreferido saves the user's default language.
func GuardarPreferido(l string) error {
	p := archivoPreferido()
	if p == "" || (l != "en" && l != "es") {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(l+"\n"), 0o644)
}

// Instruccion is the line appended to prompts whose output a person reads.
func Instruccion() string {
	if Es() {
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

// Es reports whether human-facing text should be Spanish.
func Es() bool { return fromEnv(lang) == "es" }
