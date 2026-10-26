package examiner

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Pastranauwu/loom/internal/task"
)

// stdlibComunes son los paquetes que una suite de pruebas referencia sin
// que el examinador se acuerde de declararlos. La ruta de import de un
// paquete de stdlib de un solo segmento es su propio nombre, así que
// basta la lista de nombres.
var stdlibComunes = map[string]bool{
	"errors": true, "fmt": true, "math": true, "os": true, "reflect": true,
	"sort": true, "strconv": true, "strings": true, "time": true, "bytes": true,
	"io": true, "regexp": true, "unicode": true, "slices": true, "maps": true,
}

// importsFaltantes devuelve las rutas de import que el contenido usa como
// `paquete.Símbolo` y no declara. El examinador es un modelo: escribe una
// suite que llama a `ast.Number{}` y se olvida de importar el paquete
// ast, y esa suite no compila nunca. El implementador tampoco puede
// arreglarla, porque las rutas de prueba le están vedadas: quemaba
// todos sus intentos —y la escalera de modelos— contra "undefined: ast".
//
// Los nombres se resuelven primero contra el propio módulo (los paquetes
// que la tarea declara en `usa` viven ahí) y después contra la stdlib de
// un segmento. Lo que no se resuelve se devuelve como faltante sin ruta,
// para que quien llama descarte la suite en vez de escribir una que no
// compila.
func importsFaltantes(roomPath, contenido string) (rutas []string, sinResolver []string) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "suite_test.go", contenido, parser.SkipObjectResolution)
	if err != nil {
		return nil, nil // sintaxis rota: validarSintaxis ya la descarta
	}

	declarados := map[string]bool{}
	for _, imp := range f.Imports {
		ruta := strings.Trim(imp.Path.Value, `"`)
		nombre := selector(ruta)
		if imp.Name != nil {
			nombre = imp.Name.Name
		}
		declarados[nombre] = true
	}
	// los identificadores del propio archivo (variables, tipos, funciones)
	// no son paquetes: `env.Eval(...)` con env local no necesita import
	locales := identificadoresLocales(f)

	mod := modulo(roomPath)
	vistos := map[string]bool{}
	for _, nombre := range basesDeSelector(f) {
		if declarados[nombre] || locales[nombre] || vistos[nombre] {
			continue
		}
		vistos[nombre] = true
		switch {
		case mod != "" && paqueteEnModulo(roomPath, mod, nombre) != "":
			rutas = append(rutas, paqueteEnModulo(roomPath, mod, nombre))
		case stdlibComunes[nombre]:
			rutas = append(rutas, nombre)
		default:
			sinResolver = append(sinResolver, nombre)
		}
	}
	return rutas, sinResolver
}

// basesDeSelector lista los identificadores que aparecen como base de un
// selector (`x` en `x.Y`), que es la única forma en que una suite
// referencia un paquete.
func basesDeSelector(f *ast.File) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok {
			out = append(out, id.Name)
		}
		return true
	})
	return out
}

// identificadoresLocales junta lo que el archivo declara por su cuenta:
// funciones, tipos, constantes, variables y parámetros. Con
// SkipObjectResolution el parser no resuelve nada, así que se recorre a
// mano; de más es inocuo (deja de agregar un import que no hacía falta),
// de menos mete un import que no compila.
func identificadoresLocales(f *ast.File) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch d := n.(type) {
		case *ast.FuncDecl:
			out[d.Name.Name] = true
		case *ast.ValueSpec:
			for _, id := range d.Names {
				out[id.Name] = true
			}
		case *ast.TypeSpec:
			out[d.Name.Name] = true
		case *ast.AssignStmt:
			for _, e := range d.Lhs {
				if id, ok := e.(*ast.Ident); ok {
					out[id.Name] = true
				}
			}
		case *ast.Field:
			for _, id := range d.Names {
				out[id.Name] = true
			}
		case *ast.RangeStmt:
			for _, e := range []ast.Expr{d.Key, d.Value} {
				if id, ok := e.(*ast.Ident); ok {
					out[id.Name] = true
				}
			}
		}
		return true
	})
	return out
}

// modulo lee la ruta del módulo del go.mod del cuarto.
func modulo(roomPath string) string {
	data, err := os.ReadFile(filepath.Join(roomPath, "go.mod"))
	if err != nil {
		return ""
	}
	for _, linea := range strings.Split(string(data), "\n") {
		linea = strings.TrimSpace(linea)
		if strings.HasPrefix(linea, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(linea, "module"))
		}
	}
	return ""
}

// paqueteEnModulo busca dentro del módulo un paquete que se llame nombre
// y devuelve su ruta de import, o "" si no existe. Recorre el árbol del
// cuarto: los paquetes hermanos que la tarea consume (`usa`) ya están ahí
// cuando el examinador corre, porque sus tareas terminaron antes.
func paqueteEnModulo(roomPath, mod, nombre string) string {
	var ruta string
	_ = filepath.WalkDir(roomPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil || ruta != "" {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		base := filepath.Base(p)
		if base == ".git" || base == ".loom" || base == "vendor" || strings.HasPrefix(base, "testdata") {
			return filepath.SkipDir
		}
		if paqueteReal(p) != nombre {
			return nil
		}
		rel, err := filepath.Rel(roomPath, p)
		if err != nil {
			return nil
		}
		if rel == "." {
			ruta = mod
			return filepath.SkipAll
		}
		ruta = mod + "/" + filepath.ToSlash(rel)
		return filepath.SkipAll
	})
	return ruta
}

// suiteCompleta arma la suite y le agrega los imports que el examinador
// se olvidó de declarar. Devuelve false cuando la suite referencia algo
// que no se puede resolver: es mejor dejar al implementador sin examen
// que con uno que no compila y que él no puede arreglar.
func suiteCompleta(lenguaje, roomPath, pkg, importPath string, imports, funcs []string) (string, bool) {
	contenido := armarSuite(lenguaje, pkg, importPath, imports, funcs)
	if lenguaje != "go" {
		return contenido, true
	}
	rutas, sinResolver := importsFaltantes(roomPath, contenido)
	if len(sinResolver) > 0 {
		return "", false
	}
	if len(rutas) == 0 {
		return contenido, true
	}
	contenido = armarSuite(lenguaje, pkg, importPath, append(imports, rutas...), funcs)
	// el segundo pase tiene que quedar cerrado: si sigue faltando algo,
	// la suite no compila y no vale la pena escribirla
	if faltan, quedan := importsFaltantes(roomPath, contenido); len(faltan) > 0 || len(quedan) > 0 {
		return "", false
	}
	return contenido, true
}

// Examinable reporta si loom puede escribirle un examen ciego a esa
// tarea. Importa fuera de este paquete porque la veda de pruebas —el
// implementador nunca toca las pruebas— solo tiene sentido donde hay un
// examen que proteger: en una tarea que no se puede examinar, vedarle
// además las rutas de prueba la deja imposible (nadie escribe la suite, y
// `go test ./pkg/...` sin pruebas pasa en vacío). Se decide por tarea y no
// por lenguaje: en un repo Go, `cmd/algo` es `package main` y no se puede
// examinar aunque el resto del repo sí.
//
// Es un pronóstico, no una promesa: el examen puede degradar después por
// razones que solo se ven con la respuesta del modelo en la mano.
func Examinable(root string, t task.Task, lenguaje string) bool {
	if t.ExamenEsqueleto {
		return t.ExamenVisible != "" && lenguajeDeTarea(lenguaje, t) != ""
	}
	if lenguajeDeTarea(lenguaje, t) == "typescript" || lenguajeDeTarea(lenguaje, t) == "javascript" {
		return false
	}
	if len(t.Expone) == 0 || lenguajeDeTarea(lenguaje, t) == "" {
		return false
	}
	if lenguajeDeTarea(lenguaje, t) != "go" {
		return true
	}
	dir, pkg := inferDirPkg(t.TocarSolo, root)
	switch real := paqueteReal(dir); real {
	case "":
		if n := paqueteDeExpone(t.Expone); n != "" {
			pkg = n
		}
	case "main":
		return false
	default:
		pkg = real
	}
	return pkg != "" && pkg != "main" && resolveImportPath(root, dir) != ""
}
