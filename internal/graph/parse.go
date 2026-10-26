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

// parsear devuelve los símbolos públicos con firma y los imports internos
// del repo, ya resueltos a la clave con que `claves` nombra al destino.
func parsear(f string, data []byte, modulo string) (syms, imps []string) {
	switch path.Ext(f) {
	case ".go":
		syms, imps = parsearGo(f, data, modulo)
	case ".py":
		syms, imps = parsearRegex(f, data, rePyExport, rePyImport)
	default:
		syms, imps = parsearRegex(f, data, reJsExport, reJsImport)
	}
	sort.Strings(syms)
	return syms, unicos(imps)
}

func parsearGo(f string, data []byte, modulo string) (syms, imps []string) {
	file, _ := parser.ParseFile(token.NewFileSet(), f, data, parser.SkipObjectResolution)
	if file == nil {
		return nil, nil
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
	return syms, imps
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
