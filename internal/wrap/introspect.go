package wrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Pastranauwu/loom/internal/idioma"
)

const maxAyuda = 16 << 10

// HelpCLI captures what a binary says about itself: --help, or its man page.
func HelpCLI(bin string) (string, error) {
	if !reBin.MatchString(bin) || interpretes[bin] {
		return "", fmt.Errorf(idioma.T("%q no es un binario que se pueda envolver"), bin)
	}
	ruta, err := exec.LookPath(bin)
	if err != nil {
		return "", fmt.Errorf(idioma.T("%s no está en el PATH · instálalo y reintenta"), bin)
	}
	for _, argv := range [][]string{{ruta, "--help"}, {"man", "-P", "cat", bin}} {
		ctx, cancel := context.WithTimeout(context.Background(), tiempoPrueba)
		var buf bytes.Buffer
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Stdout, cmd.Stderr = &buf, &buf
		_ = cmd.Run() // la ayuda sale con código distinto de 0 en muchos CLIs: manda el texto
		cancel()
		if s := strings.TrimSpace(buf.String()); s != "" {
			if len(s) > maxAyuda {
				s = s[:maxAyuda]
			}
			return s, nil
		}
	}
	return "", fmt.Errorf(idioma.T("%s no devolvió ayuda con --help ni man"), bin)
}

// Request is one HTTP call reduced to what a tool needs to reproduce it.
// Secret header values are replaced by a {placeholder}.
type Request struct {
	Method        string            `json:"method"`
	URL           string            `json:"url"`
	Headers       map[string]string `json:"headers,omitempty"`
	Body          string            `json:"body,omitempty"`
	BodyShape     any               `json:"body_shape,omitempty"`
	ResponseShape any               `json:"response_shape,omitempty"`
}

var secretos = map[string]bool{"authorization": true, "x-api-key": true, "cookie": true, "proxy-authorization": true, "x-auth-token": true}

func (r *Request) header(nombre, valor string) {
	if r.Headers == nil {
		r.Headers = map[string]string{}
	}
	if secretos[strings.ToLower(nombre)] {
		valor = "{" + Slug(nombre) + "}"
	}
	r.Headers[nombre] = valor
}

// Shape describes the structure of a JSON value without its data.
func Shape(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[k] = Shape(e)
		}
		return m
	case []any:
		if len(x) == 0 {
			return []any{}
		}
		return []any{Shape(x[0])}
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	}
	return "null"
}

func shapeDe(texto string) any {
	var v any
	if json.Unmarshal([]byte(texto), &v) != nil {
		return nil
	}
	return Shape(v)
}

// ParseCurl reads a cURL command, or a file holding one. The line is split
// like a shell would but nothing is executed.
func ParseCurl(src string) (Request, error) {
	if b, err := os.ReadFile(src); err == nil {
		src = string(b)
	}
	toks, err := tokenizar(strings.ReplaceAll(src, "\\\n", " "))
	if err != nil {
		return Request{}, err
	}
	if len(toks) > 0 && toks[0] == "curl" {
		toks = toks[1:]
	}
	var r Request
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		sig := func() string {
			if i+1 < len(toks) {
				i++
				return toks[i]
			}
			return ""
		}
		switch t {
		case "-X", "--request":
			r.Method = strings.ToUpper(sig())
		case "-H", "--header":
			if k, v, ok := strings.Cut(sig(), ":"); ok {
				r.header(strings.TrimSpace(k), strings.TrimSpace(v))
			}
		case "-d", "--data", "--data-raw", "--data-binary", "--data-ascii":
			r.Body = sig()
		case "--json":
			r.Body = sig()
			r.header("Content-Type", "application/json")
		case "-u", "--user":
			sig()
			r.header("Authorization", "")
		case "--url":
			r.URL = sig()
		case "-o", "--output", "-A", "--user-agent", "-m", "--max-time", "-e", "--referer", "-b", "--cookie":
			sig()
		default:
			if !strings.HasPrefix(t, "-") && r.URL == "" {
				r.URL = t
			}
		}
	}
	if r.URL == "" {
		return r, errors.New(idioma.T("el cURL no trae url · pega un comando completo"))
	}
	if r.Method == "" {
		r.Method = "GET"
		if r.Body != "" {
			r.Method = "POST"
		}
	}
	r.BodyShape = shapeDe(r.Body)
	return r, nil
}

func tokenizar(s string) ([]string, error) {
	var toks []string
	var cur strings.Builder
	var comilla rune
	activo := false
	for _, c := range s {
		switch {
		case comilla != 0:
			if c == comilla {
				comilla = 0
			} else {
				cur.WriteRune(c)
			}
		case c == '\'' || c == '"':
			comilla, activo = c, true
		case c == ' ' || c == '\t' || c == '\n':
			if activo || cur.Len() > 0 {
				toks = append(toks, cur.String())
				cur.Reset()
				activo = false
			}
		default:
			cur.WriteRune(c)
		}
	}
	if comilla != 0 {
		return nil, errors.New(idioma.T("el cURL tiene una comilla sin cerrar"))
	}
	if activo || cur.Len() > 0 {
		toks = append(toks, cur.String())
	}
	return toks, nil
}

// ParseHAR reads a HAR capture: up to maxHAR distinct API calls, skipping
// static assets.
func ParseHAR(ruta string) ([]Request, error) {
	const maxHAR = 12
	b, err := os.ReadFile(ruta)
	if err != nil {
		return nil, fmt.Errorf(idioma.T("no pude leer %s · %w"), ruta, err)
	}
	var har struct {
		Log struct {
			Entries []struct {
				Request struct {
					Method   string                         `json:"method"`
					URL      string                         `json:"url"`
					Headers  []struct{ Name, Value string } `json:"headers"`
					PostData struct{ Text string }          `json:"postData"`
				} `json:"request"`
				Response struct {
					Content struct {
						Text, MimeType, Encoding string
					} `json:"content"`
				} `json:"response"`
			} `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal(b, &har); err != nil {
		return nil, fmt.Errorf(idioma.T("%s no es un HAR válido · %w"), ruta, err)
	}
	vistos := map[string]bool{}
	var out []Request
	for _, e := range har.Log.Entries {
		mime := e.Response.Content.MimeType
		if mime != "" && !strings.Contains(mime, "json") {
			continue
		}
		clave := e.Request.Method + " " + strings.SplitN(e.Request.URL, "?", 2)[0]
		if vistos[clave] || len(out) >= maxHAR {
			continue
		}
		vistos[clave] = true
		r := Request{Method: e.Request.Method, URL: e.Request.URL, Body: e.Request.PostData.Text}
		for _, h := range e.Request.Headers {
			if !strings.HasPrefix(h.Name, ":") && !ruido[strings.ToLower(h.Name)] {
				r.header(h.Name, h.Value)
			}
		}
		r.BodyShape = shapeDe(r.Body)
		if e.Response.Content.Encoding == "" {
			r.ResponseShape = shapeDe(e.Response.Content.Text)
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, errors.New(idioma.T("el HAR no trae llamadas JSON que envolver"))
	}
	return out, nil
}

var ruido = map[string]bool{"user-agent": true, "accept-encoding": true, "accept-language": true, "connection": true, "host": true,
	"referer": true, "origin": true, "content-length": true, "sec-fetch-mode": true, "sec-fetch-site": true, "sec-fetch-dest": true}

// Resumen is the request list as the prompt sees it.
func Resumen(rs ...Request) string {
	b, _ := json.MarshalIndent(rs, "", "  ")
	return string(b)
}
