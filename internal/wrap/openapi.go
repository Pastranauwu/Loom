package wrap

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Pastranauwu/loom/internal/idioma"
)

const maxHerramientas = 40

var verbos = []string{"get", "post", "put", "patch", "delete"}

// FromOpenAPI converts an OpenAPI 3 or Swagger 2 spec (JSON or YAML, file or
// URL) into a manifest with no model call. Only required parameters and
// required scalar body fields become template variables; the rest is left
// out and reported in warnings.
func FromOpenAPI(src, name string) (Manifest, []string, error) {
	b, err := leerSpec(src)
	if err != nil {
		return Manifest{}, nil, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil || doc["paths"] == nil {
		return Manifest{}, nil, fmt.Errorf(idioma.T("%s no es un spec OpenAPI con paths"), src)
	}
	base, err := baseURL(doc, src)
	if err != nil {
		return Manifest{}, nil, err
	}
	info := obj(doc["info"])
	if name == "" {
		name, _ = info["title"].(string)
	}
	m := Manifest{Name: Slug(name), Description: cortar(fmt.Sprint(info["description"]), 200)}
	if m.Description == "<nil>" || m.Description == "" {
		m.Description = "OpenAPI bridge for " + base
	}
	var avisos []string
	if obj(obj(doc["components"])["securitySchemes"]) != nil || doc["securityDefinitions"] != nil {
		avisos = append(avisos, idioma.T("· el spec declara autenticación · agrega a mano los encabezados al puente"))
	}
	paths := obj(doc["paths"])
	rutas := make([]string, 0, len(paths))
	for p := range paths {
		rutas = append(rutas, p)
	}
	sort.Strings(rutas)
	usados := map[string]bool{}
	for _, ruta := range rutas {
		item := obj(paths[ruta])
		for _, verbo := range verbos {
			op := obj(item[verbo])
			if op == nil {
				continue
			}
			if len(m.Tools) >= maxHerramientas {
				avisos = append(avisos, fmt.Sprintf(idioma.T("· solo se envolvieron las primeras %d operaciones"), maxHerramientas))
				return m, avisos, nil
			}
			t, w := operacion(doc, base, ruta, verbo, item, op)
			avisos = append(avisos, w...)
			for n := t.Name; usados[t.Name]; {
				n += "_"
				t.Name = n
			}
			usados[t.Name] = true
			m.Tools = append(m.Tools, t)
		}
	}
	return m, avisos, nil
}

func operacion(doc map[string]any, base, ruta, verbo string, item, op map[string]any) (Tool, []string) {
	nombre, _ := op["operationId"].(string)
	if nombre == "" {
		nombre = verbo + "_" + ruta
	}
	desc, _ := op["summary"].(string)
	if d, _ := op["description"].(string); desc == "" {
		desc = d
	}
	t := Tool{Name: Slug(nombre), Description: cortar(desc, 300)}
	props, req := map[string]any{}, []string{}
	h := &HTTP{Method: strings.ToUpper(verbo), URL: base + ruta}
	var query []string
	var avisos []string
	params, _ := item["parameters"].([]any)
	opParams, _ := op["parameters"].([]any)
	var esquemaCuerpo map[string]any
	for _, raw := range append(append([]any{}, params...), opParams...) {
		p := resolver(doc, obj(raw), 0)
		pn, _ := p["name"].(string)
		donde, _ := p["in"].(string)
		if donde == "body" { // swagger 2
			esquemaCuerpo = resolver(doc, obj(p["schema"]), 0)
			continue
		}
		if pn == "" {
			continue
		}
		if r, _ := p["required"].(bool); !r && donde != "path" {
			continue
		}
		v := varDe(pn)
		sch := p
		if s := obj(p["schema"]); s != nil {
			sch = resolver(doc, s, 0)
		}
		props[v] = propiedad(sch, p["description"])
		req = append(req, v)
		switch donde {
		case "path":
			h.URL = strings.ReplaceAll(h.URL, "{"+pn+"}", "{"+v+"}")
		case "query":
			query = append(query, url.QueryEscape(pn)+"={"+v+"}")
		case "header":
			if h.Headers == nil {
				h.Headers = map[string]string{}
			}
			h.Headers[pn] = "{" + v + "}"
		}
	}
	if rb := obj(op["requestBody"]); rb != nil {
		rb = resolver(doc, rb, 0)
		esquemaCuerpo = resolver(doc, obj(obj(obj(rb["content"])["application/json"])["schema"]), 0)
	}
	if esquemaCuerpo != nil {
		var campos []string
		obligatorios := strSet(esquemaCuerpo["required"])
		bp := obj(esquemaCuerpo["properties"])
		nombres := make([]string, 0, len(bp))
		for k := range bp {
			nombres = append(nombres, k)
		}
		sort.Strings(nombres)
		for _, k := range nombres {
			sch := resolver(doc, obj(bp[k]), 0)
			tipo, _ := sch["type"].(string)
			if !obligatorios[k] {
				continue
			}
			v := varDe(k)
			if tipo == "object" || tipo == "array" {
				avisos = append(avisos, fmt.Sprintf(idioma.T("· %s · el campo %s es %s y se omite del cuerpo"), t.Name, k, tipo))
				continue
			}
			props[v] = propiedad(sch, sch["description"])
			req = append(req, v)
			ph := `"{` + v + `}"`
			if tipo == "integer" || tipo == "number" || tipo == "boolean" {
				ph = "{" + v + "}"
			}
			campos = append(campos, fmt.Sprintf("%q:%s", k, ph))
		}
		if len(campos) > 0 {
			h.Body = "{" + strings.Join(campos, ",") + "}"
			if h.Headers == nil {
				h.Headers = map[string]string{}
			}
			h.Headers["Content-Type"] = "application/json"
		}
	}
	if len(query) > 0 {
		h.URL += "?" + strings.Join(query, "&")
	}
	t.InputSchema = map[string]any{"type": "object", "properties": props, "required": req}
	t.HTTP = h
	return t, avisos
}

func propiedad(sch map[string]any, desc any) map[string]any {
	tipo, _ := sch["type"].(string)
	if tipo != "integer" && tipo != "number" && tipo != "boolean" {
		tipo = "string"
	}
	p := map[string]any{"type": tipo}
	if d, ok := desc.(string); ok && d != "" {
		p["description"] = cortar(d, 200)
	}
	return p
}

func leerSpec(src string) ([]byte, error) {
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		c := http.Client{Timeout: 30 * time.Second}
		resp, err := c.Get(src)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf(idioma.T("%s respondió %s"), src, resp.Status)
		}
		return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return nil, fmt.Errorf(idioma.T("no pude leer %s · %w"), src, err)
	}
	return b, nil
}

func baseURL(doc map[string]any, src string) (string, error) {
	var base string
	if servers, _ := doc["servers"].([]any); len(servers) > 0 {
		base, _ = obj(servers[0])["url"].(string)
	} else if host, _ := doc["host"].(string); host != "" { // swagger 2
		esquema := "https"
		if s, _ := doc["schemes"].([]any); len(s) > 0 {
			esquema = fmt.Sprint(s[0])
		}
		bp, _ := doc["basePath"].(string)
		base = esquema + "://" + host + bp
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		u, err := url.Parse(src)
		if err != nil || u.Host == "" {
			return "", fmt.Errorf(idioma.T("el spec no declara un servidor absoluto · agrega servers con una url http(s)"))
		}
		rel, err := url.Parse(base)
		if err != nil {
			return "", err
		}
		base = u.ResolveReference(rel).String()
	}
	return strings.TrimRight(base, "/"), nil
}

// obj returns v as a string-keyed map, or nil.
func obj(v any) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		return x
	case map[any]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[fmt.Sprint(k)] = e
		}
		return m
	}
	return nil
}

// resolver follows "#/a/b" references inside the doc.
func resolver(doc, m map[string]any, depth int) map[string]any {
	ref, _ := m["$ref"].(string)
	if ref == "" || !strings.HasPrefix(ref, "#/") || depth > 8 {
		return m
	}
	var cur any = doc
	for _, seg := range strings.Split(ref[2:], "/") {
		cur = obj(cur)[seg]
	}
	if r := obj(cur); r != nil {
		return resolver(doc, r, depth+1)
	}
	return m
}

func varDe(s string) string {
	v := reNoVar.ReplaceAllString(s, "_")
	if v == "" || (v[0] >= '0' && v[0] <= '9') {
		v = "_" + v
	}
	return v
}

func strSet(v any) map[string]bool {
	out := map[string]bool{}
	l, _ := v.([]any)
	for _, e := range l {
		out[fmt.Sprint(e)] = true
	}
	return out
}

func cortar(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}
