package graph

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func moduloGo(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(data), "\n") {
		if m, ok := strings.CutPrefix(strings.TrimSpace(l), "module "); ok {
			return strings.Trim(strings.TrimSpace(m), `"`)
		}
	}
	return ""
}

// parsear devuelve el propósito declarado (primera frase del comentario
// del paquete o del archivo), los símbolos públicos con firma y los imports
// internos del repo, ya resueltos a la clave con que `claves` nombra al destino.
func parsear(f string, data []byte, modulo string) (doc string, syms, imps []string) {
	switch path.Ext(f) {
	case ".go":
		doc, syms, imps = parsearGo(f, data, modulo)
	case ".py":
		syms, imps = parsearRegex(f, data, rePyExport, rePyImport)
		if doc = primerGrupo(rePyComentario, data); doc == "" {
			doc = primerGrupo(rePyDoc, data)
		}
	default:
		syms, imps = parsearRegex(f, data, reJsExport, reJsImport)
		doc = primerGrupo(reJsDoc, data)
	}
	sort.Strings(syms)
	return primeraFrase(doc), syms, unicos(imps)
}

// Doc es el propósito que declara un archivo: la primera frase de su
// comentario de paquete (Go), docstring de módulo (Python) o comentario
// inicial (JS/TS). Vacío si no declara ninguno.
func Doc(f string, data []byte) string {
	doc, _, _ := parsear(f, data, "")
	return doc
}

var (
	// un bloque "#" al inicio, tras el shebang y la línea de coding
	rePyComentario = regexp.MustCompile(`\A(?:#![^\n]*\n)?(?:#[^\n]*coding[:=][^\n]*\n)?((?:[ \t]*#(?:[^!\n][^\n]*)?\n)+)`)
	rePyDoc        = regexp.MustCompile(`\A(?:\s*#[^\n]*\n)*\s*[rRuU]?(?:"""|''')([\s\S]*?)(?:"""|''')`)
	reJsDoc        = regexp.MustCompile(`\A(?:#![^\n]*\n)?\s*(?:/\*\*?([\s\S]*?)\*/|((?:[ \t]*//[^\n]*\n)+))`)
	reMarcas       = regexp.MustCompile(`(?m)^\s*(?://+|\*+|#+)\s?`)
)

func primerGrupo(re *regexp.Regexp, data []byte) string {
	m := re.FindSubmatch(data)
	for _, g := range m[min(1, len(m)):] {
		if len(g) > 0 {
			return reMarcas.ReplaceAllString(string(g), "")
		}
	}
	return ""
}

// primeraFrase deja una línea corta: sin "Package x", hasta el primer punto
// seguido de espacio y a lo más 160 runas. Es lo que lee el modelo por
// módulo, así que pesa tokens en cada prompt.
func primeraFrase(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if strings.HasPrefix(s, "Package ") {
		if p := strings.SplitN(s, " ", 3); len(p) == 3 {
			s = p[2]
		}
	}
	if i := strings.Index(s, ". "); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSuffix(s, ".")
	if r := []rune(s); len(r) > 160 {
		s = string(r[:159]) + "…"
	}
	return s
}

func parsearGo(f string, data []byte, modulo string) (doc string, syms, imps []string) {
	file, _ := parser.ParseFile(token.NewFileSet(), f, data, parser.SkipObjectResolution|parser.ParseComments)
	if file == nil {
		return "", nil, nil
	}
	if file.Doc != nil {
		doc = file.Doc.Text()
	}
	for _, s := range file.Imports {
		p, _ := strconv.Unquote(s.Path.Value)
		if rel, ok := strings.CutPrefix(p, modulo+"/"); ok && modulo != "" {
			imps = append(imps, rel)
		}
	}
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			recv := ""
			if d.Recv != nil && len(d.Recv.List) > 0 {
				recv = "(" + types.ExprString(d.Recv.List[0].Type) + ") "
			}
			syms = append(syms, "func "+recv+d.Name.Name+strings.TrimPrefix(types.ExprString(d.Type), "func"))
		case *ast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					if s.Name.IsExported() {
						syms = append(syms, "type "+s.Name.Name+" "+tipoDe(s.Type))
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.IsExported() {
							syms = append(syms, d.Tok.String()+" "+n.Name)
						}
					}
				}
			}
		}
	}
	return doc, syms, imps
}

func tipoDe(e ast.Expr) string {
	switch e.(type) {
	case *ast.StructType:
		return "struct"
	case *ast.InterfaceType:
		return "interface"
	}
	return types.ExprString(e)
}

var (
	rePyExport = regexp.MustCompile(`(?m)^(?:async\s+)?(?:def|class)\s+[A-Za-z]\w*[^\n]*`)
	rePyImport = regexp.MustCompile(`(?m)^\s*(?:from\s+(\.*[\w.]*)\s+import|import\s+([\w.]+))`)
	reJsExport = regexp.MustCompile(`(?m)^export\s+(?:default\s+)?(?:async\s+)?(?:function|class|const|let|interface|type|enum)\s+\w+[^\n{=]*`)
	reJsImport = regexp.MustCompile(`(?:from|import|require\()\s*['"](\.[^'"]*)['"]`)
)

func parsearRegex(f string, data []byte, export, imp *regexp.Regexp) (syms, imps []string) {
	src := string(data)
	for _, m := range export.FindAllString(src, -1) {
		m = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(m), ":"))
		if !strings.Contains(m, " _") {
			syms = append(syms, m)
		}
	}
	dir := path.Dir(f)
	for _, m := range imp.FindAllStringSubmatch(src, -1) {
		if path.Ext(f) == ".py" {
			imps = append(imps, resolverPy(dir, m[1]+m[2]))
		} else {
			imps = append(imps, strings.TrimSuffix(path.Join(dir, m[1]), path.Ext(m[1])))
		}
	}
	return syms, imps
}

// resolverPy convierte "a.b" o "..a" en una ruta del repo; los imports
// absolutos que no existen como carpeta del repo no enlazan con nada.
func resolverPy(dir, mod string) string {
	puntos := len(mod) - len(strings.TrimLeft(mod, "."))
	rest := strings.ReplaceAll(strings.TrimLeft(mod, "."), ".", "/")
	if puntos == 0 {
		return rest
	}
	base := dir
	for i := 1; i < puntos; i++ {
		base = path.Dir(base)
	}
	return path.Join(base, rest)
}

func unicos(s []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range s {
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}
