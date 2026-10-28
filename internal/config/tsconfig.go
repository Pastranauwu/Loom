package config

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// opcionesTSObsoletas son las que TypeScript 5 declaró deprecadas: con
// ellas el compilador avisa o se niega según la versión, y la suite rebota.
var opcionesTSObsoletas = []string{
	"importsNotUsedAsValues", "preserveValueImports", "keyofStringsOnly", "suppressImplicitAnyIndexErrors",
	"suppressExcessPropertyErrors", "noStrictGenericChecks", "noImplicitUseStrict", "out", "charset", "prepend",
}

var resolucionesObsoletas = map[string]bool{"node": true, "node10": true, "classic": true}

// TsconfigObsoleto lee los tsconfig*.json del proyecto (hasta tres niveles,
// sin node_modules) y devuelve qué opción deprecada trae cada uno. Un
// archivo que no parsea no se juzga: tsc lo dirá con su propio mensaje.
func TsconfigObsoleto(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() {
			if n := d.Name(); p != dir && (n == "node_modules" || strings.HasPrefix(n, ".") || n == "dist" || n == "build" || strings.Count(rel, string(filepath.Separator)) >= 2) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasPrefix(d.Name(), "tsconfig") || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		for _, problema := range ProblemasTsconfig(data) {
			out = append(out, filepath.ToSlash(rel)+": "+problema)
		}
		return nil
	})
	return out
}

// ProblemasTsconfig revisa un tsconfig (JSONC: con comentarios y comas finales).
func ProblemasTsconfig(data []byte) []string {
	var cfg struct {
		CompilerOptions map[string]any `json:"compilerOptions"`
	}
	if json.Unmarshal(sinJSONC(data), &cfg) != nil {
		return nil
	}
	var out []string
	opts := map[string]any{}
	for k, v := range cfg.CompilerOptions {
		opts[strings.ToLower(k)] = v
	}
	if r, ok := opts["moduleresolution"].(string); ok && resolucionesObsoletas[strings.ToLower(r)] {
		out = append(out, fmt.Sprintf("moduleResolution %q is deprecated · use \"bundler\" (apps) or \"node16\"/\"nodenext\" (Node libraries)", r))
	}
	if t, ok := opts["target"].(string); ok && strings.EqualFold(t, "es3") {
		out = append(out, "target \"ES3\" is deprecated · use ES2020 or later")
	}
	for _, o := range opcionesTSObsoletas {
		if _, ok := opts[strings.ToLower(o)]; ok {
			out = append(out, o+" is deprecated in TypeScript 5 · remove it")
		}
	}
	sort.Strings(out)
	return out
}

var reComaFinal = regexp.MustCompile(`,(\s*[}\]])`)

// sinJSONC quita comentarios // y /* */ fuera de las cadenas, y las comas finales.
func sinJSONC(data []byte) []byte {
	var b strings.Builder
	s := string(data)
	enCadena := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case enCadena:
			b.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
			} else if c == '"' {
				enCadena = false
			}
		case c == '"':
			enCadena = true
			b.WriteByte(c)
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			for i < len(s) && s[i] != '\n' {
				i++
			}
			b.WriteByte('\n')
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			fin := strings.Index(s[i+2:], "*/")
			if fin < 0 {
				return []byte(b.String())
			}
			i += fin + 3
		default:
			b.WriteByte(c)
		}
	}
	return []byte(reComaFinal.ReplaceAllString(b.String(), "$1"))
}
