package wrap

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Pastranauwu/loom/internal/idioma"
)

// interpretes can run arbitrary text as a program, so a bridge may not
// start them: the template would become a shell injection point.
var interpretes = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "ash": true, "ksh": true, "fish": true,
	"csh": true, "tcsh": true, "cmd": true, "cmd.exe": true, "powershell": true, "powershell.exe": true,
	"pwsh": true, "env": true, "xargs": true, "sudo": true, "doas": true, "eval": true, "nohup": true,
}

var (
	reBin        = regexp.MustCompile(`^[A-Za-z0-9_./+-]+$`)
	metodos      = map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "HEAD": true}
	tiempoPrueba = 10 * time.Second
)

// Verify is the gate every manifest crosses before it is saved. Errors
// reject it; warnings are printed by the caller. smoke runs each binary
// with --version or --help.
func Verify(m Manifest, smoke bool) (warnings []string, err error) {
	if !reNombre.MatchString(m.Name) {
		return nil, fmt.Errorf(idioma.T("nombre de puente inválido: %q · usa minúsculas, dígitos y _"), m.Name)
	}
	if len(m.Tools) == 0 {
		return nil, errors.New(idioma.T("el puente no trae herramientas · vuelve a intentarlo"))
	}
	vistos, bins := map[string]bool{}, map[string]bool{}
	for _, t := range m.Tools {
		if !reNombre.MatchString(t.Name) {
			return nil, fmt.Errorf(idioma.T("herramienta %q con nombre inválido · usa minúsculas, dígitos y _"), t.Name)
		}
		if vistos[t.Name] {
			return nil, fmt.Errorf(idioma.T("herramienta %q repetida"), t.Name)
		}
		vistos[t.Name] = true
		if (t.Exec == nil) == (t.HTTP == nil) {
			return nil, fmt.Errorf(idioma.T("herramienta %s · debe traer exec o http, uno solo"), t.Name)
		}
		props, req, err := esquema(t)
		if err != nil {
			return nil, err
		}
		var plantillas []string
		if t.Exec != nil {
			if err := verificarExec(t.Name, *t.Exec); err != nil {
				return nil, err
			}
			bins[t.Exec.Bin] = true
			plantillas = append([]string{t.Exec.Bin}, t.Exec.Args...)
		} else {
			if err := verificarHTTP(t.Name, *t.HTTP); err != nil {
				return nil, err
			}
			plantillas = []string{t.HTTP.URL, t.HTTP.Body}
			for k, v := range t.HTTP.Headers {
				plantillas = append(plantillas, k, v)
			}
		}
		for _, p := range plantillas {
			for _, v := range reVar.FindAllStringSubmatch(p, -1) {
				if _, ok := props[v[1]]; !ok {
					return nil, fmt.Errorf(idioma.T("herramienta %s · la variable {%s} no es propiedad del inputSchema"), t.Name, v[1])
				}
				if !req[v[1]] {
					return nil, fmt.Errorf(idioma.T("herramienta %s · la variable {%s} debe estar en required"), t.Name, v[1])
				}
			}
		}
	}
	if smoke {
		for bin := range bins {
			w, err := pruebaDeHumo(bin)
			if err != nil {
				return warnings, err
			}
			warnings = append(warnings, w...)
		}
	}
	return warnings, nil
}

// esquema returns the properties and required names of a tool's inputSchema.
func esquema(t Tool) (props map[string]any, req map[string]bool, err error) {
	if t.InputSchema["type"] != "object" {
		return nil, nil, fmt.Errorf(idioma.T("herramienta %s · inputSchema debe ser de tipo object"), t.Name)
	}
	props, _ = t.InputSchema["properties"].(map[string]any)
	req = map[string]bool{}
	switch r := t.InputSchema["required"].(type) {
	case []any:
		for _, v := range r {
			if s, ok := v.(string); ok {
				req[s] = true
			}
		}
	case []string:
		for _, s := range r {
			req[s] = true
		}
	}
	return props, req, nil
}

func verificarExec(tool string, e Exec) error {
	base := strings.ToLower(filepath.Base(e.Bin))
	switch {
	case !reBin.MatchString(e.Bin):
		return fmt.Errorf(idioma.T("herramienta %s · bin %q no es un nombre de binario simple"), tool, e.Bin)
	case interpretes[base]:
		return fmt.Errorf(idioma.T("herramienta %s · %s ejecuta texto como programa · los puentes no pasan por una shell"), tool, base)
	}
	return nil
}

func verificarHTTP(tool string, h HTTP) error {
	if !metodos[strings.ToUpper(h.Method)] {
		return fmt.Errorf(idioma.T("herramienta %s · método %q no permitido"), tool, h.Method)
	}
	if !strings.HasPrefix(h.URL, "http://") && !strings.HasPrefix(h.URL, "https://") {
		return fmt.Errorf(idioma.T("herramienta %s · la url debe empezar con http:// o https://"), tool)
	}
	return nil
}

// pruebaDeHumo exige que el binario exista; que no responda a --version ni
// a --help solo avisa, porque hay CLIs que salen distinto de 0 al pedir ayuda.
func pruebaDeHumo(bin string) ([]string, error) {
	ruta, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf(idioma.T("%s no está en el PATH · instálalo y reintenta"), bin)
	}
	for _, flag := range []string{"--version", "--help"} {
		ctx, cancel := context.WithTimeout(context.Background(), tiempoPrueba)
		err := exec.CommandContext(ctx, ruta, flag).Run()
		cancel()
		if err == nil {
			return nil, nil
		}
	}
	return []string{fmt.Sprintf(idioma.T("· %s existe pero no respondió a --version ni --help"), bin)}, nil
}
