package wrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Pastranauwu/loom/internal/idioma"
)

const (
	maxSalida   = 64 << 10
	tiempoCorre = 60 * time.Second
)

// Invoke runs one tool with the arguments the MCP client sent. Arguments
// are type-checked against the inputSchema and substituted element by
// element: a value never becomes more than one argv entry.
func Invoke(t Tool, args map[string]any) (string, error) {
	props, req, err := esquema(t)
	if err != nil {
		return "", err
	}
	for k := range req {
		if _, ok := args[k]; !ok {
			return "", fmt.Errorf(idioma.T("falta el argumento %s"), k)
		}
	}
	for k, v := range args {
		if p, ok := props[k].(map[string]any); ok {
			if err := tipoOK(k, p["type"], v); err != nil {
				return "", err
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), tiempoCorre)
	defer cancel()
	if t.Exec != nil {
		return correr(ctx, *t.Exec, args)
	}
	return pedir(ctx, *t.HTTP, args)
}

func tipoOK(k string, tipo, v any) error {
	ok := true
	switch tipo {
	case "string":
		_, ok = v.(string)
	case "number":
		_, ok = v.(float64)
	case "integer":
		f, isNum := v.(float64)
		ok = isNum && f == float64(int64(f))
	case "boolean":
		_, ok = v.(bool)
	}
	if !ok {
		return fmt.Errorf(idioma.T("el argumento %s debe ser %v"), k, tipo)
	}
	return nil
}

func texto(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// sustituir reemplaza {var} en una pasada: lo sustituido no se vuelve a expandir.
func sustituir(s string, args map[string]any, esc func(string) string) string {
	return reVar.ReplaceAllStringFunc(s, func(m string) string {
		v, ok := args[m[1:len(m)-1]]
		if !ok {
			return m
		}
		if esc == nil {
			return texto(v)
		}
		return esc(texto(v))
	})
}

func correr(ctx context.Context, e Exec, args map[string]any) (string, error) {
	argv := make([]string, len(e.Args))
	for i, a := range e.Args {
		argv[i] = sustituir(a, args, nil)
	}
	cmd := exec.CommandContext(ctx, e.Bin, argv...)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	s := recortar(buf.String())
	if err != nil {
		return "", errors.New(strings.TrimSpace(s + "\n" + err.Error()))
	}
	return s, nil
}

func pedir(ctx context.Context, h HTTP, args map[string]any) (string, error) {
	body := sustituir(h.Body, args, func(s string) string {
		b, _ := json.Marshal(s) // el cuerpo es JSON: la plantilla pone las comillas
		return string(b[1 : len(b)-1])
	})
	r, err := http.NewRequestWithContext(ctx, strings.ToUpper(h.Method), sustituir(h.URL, args, url.QueryEscape), strings.NewReader(body))
	if err != nil {
		return "", err
	}
	for k, v := range h.Headers {
		v = sustituir(v, args, nil)
		if strings.ContainsAny(v, "\r\n") {
			return "", fmt.Errorf(idioma.T("el encabezado %s trae un salto de línea"), k)
		}
		r.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, maxSalida+1))
	s := recortar(string(b))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("%s\n%s", resp.Status, s)
	}
	return s, nil
}

func recortar(s string) string {
	if len(s) > maxSalida {
		return s[:maxSalida] + "\n… truncated"
	}
	return s
}
