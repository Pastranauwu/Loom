package ship

import (
	"path"
	"regexp"
	"strings"

	"github.com/Pastranauwu/loom/internal/task"
)

// escanearSecretos busca credenciales en las líneas añadidas.
// Patrones de alta señal: claves de proveedores, claves privadas y
// asignaciones literales de contraseñas. Las keys nunca entran al PR.
func escanearSecretos(diff string) []Hallazgo {
	var h []Hallazgo
	for _, ad := range parseDiffAnadido(diff) {
		for _, linea := range ad.lineas {
			if nombre := tipoSecreto(ad.nombre, linea); nombre != "" {
				h = append(h, Hallazgo{Tipo: nombre, Archivo: ad.nombre})
			}
		}
	}
	return h
}

var patronesSecretos = []struct {
	nombre string
	re     *regexp.Regexp
}{
	{"clave AWS", regexp.MustCompile(`\b(AKIA|ASIA)[A-Z0-9]{16}\b`)},
	{"clave privada", regexp.MustCompile(`-----BEGIN (RSA |EC |OPENSSH |DSA |PGP )?PRIVATE KEY-----`)},
	{"token GitHub", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`)},
	{"clave de API de Google", regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}\b`)},
	{"clave Stripe", regexp.MustCompile(`\bsk_(live|test)_[0-9a-zA-Z]{24}\b`)},
	{"token Slack", regexp.MustCompile(`\bxox[baprs]-[0-9a-zA-Z-]{10,}\b`)},
	{"clave Anthropic", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_\-]{24,}`)},
	{"clave OpenAI", regexp.MustCompile(`\bsk-proj-[A-Za-z0-9_\-]{24,}`)},
	{"clave OpenAI", regexp.MustCompile(`\bsk-[A-Za-z0-9]{32,}\b`)},
	{"token Hugging Face", regexp.MustCompile(`\bhf_[A-Za-z0-9]{32,}\b`)},
}

// reAsignacion captura `NOMBRE = valor` con o SIN comillas. El prefijo
// [A-Za-z0-9_]* es lo que hace que DB_PASSWORD cuente: `\bpassword` no
// matchea ahi porque `_` es caracter de palabra y no abre frontera. Un .env no
// las lleva, que es justo donde viven las credenciales: exigirlas dejaba
// pasar DB_PASSWORD=... sin decir nada.
//
// El valor sale capturado en vez de validarse dentro del regex porque lo
// que descarta un falso positivo no es su forma sino qué es: una llamada
// a función, una referencia a variable de entorno o un placeholder. Eso
// se lee mejor en Go que en una alternancia.
var reAsignacion = regexp.MustCompile(`(?i)\b[A-Za-z0-9_]*(api[_-]?key|secret(?:_key)?|password|passwd|token|auth)\s*[:=]\s*(?:'([^']{8,})'|"([^"]{8,})"|([^\s'"` + "`" + `,;]{8,}))`)

// esSecretoLiteral reporta si el valor de una asignación es una
// credencial escrita a mano, y no la forma de conseguirla.
func esSecretoLiteral(v string) bool {
	if v == "" {
		return false
	}
	// referencia, no literal: $VAR, ${VAR}, %VAR%, <poné-tu-clave>
	switch v[0] {
	case '$', '{', '<', '%', '@':
		return false
	}
	// llamada a función: os.Getenv("X"), process.env.X, config.Get(...)
	if strings.ContainsAny(v, "()") || strings.Contains(v, "process.env") {
		return false
	}
	// tipo o campo de struct, no un valor
	switch strings.ToLower(v) {
	case "string", "str", "bytes", "nil", "none", "null", "true", "false", "required", "optional":
		return false
	}
	// placeholder de documentación: el propósito es que NO sea real
	bajo := strings.ToLower(v)
	for _, marca := range []string{
		"changeme", "your-", "your_", "yourkey", "tu-clave", "tu_clave",
		"xxxxx", "example", "ejemplo", "placeholder", "redacted",
		"dummy", "fake", "sample", "test-value", "...", "***",
	} {
		if strings.Contains(bajo, marca) {
			return false
		}
	}
	// sin al menos un dígito o un guion/underscore interno, casi siempre
	// es una palabra suelta ("password = admin" lo dejamos pasar: no es
	// una fuga, es un default de ejemplo que ya cazan otras reglas)
	return strings.ContainsAny(v, "0123456789-_./+")
}

func tipoSecreto(archivo, linea string) string {
	for _, p := range patronesSecretos {
		if p.re.MatchString(linea) {
			return p.nombre
		}
	}
	// una prueba lleva claves falsas a propósito ("sk-or-secret"); una
	// real ahí la cazan los patrones de proveedor de arriba
	if task.EsArchivoDePrueba(archivo) || esPlantilla(archivo) {
		return ""
	}
	if m := reAsignacion.FindStringSubmatch(linea); m != nil {
		// grupos 2, 3 y 4 son las tres formas del valor: comilla simple,
		// comilla doble y sin comillas. Solo una viene llena. Sin
		// comillas solo es literal en un archivo de configuración: en
		// código, `api_key=settings.api_key` es una variable.
		for i, v := range m[2:] {
			if i == 2 && !esConfiguracion(archivo) {
				continue
			}
			if v != "" && esSecretoLiteral(v) {
				return "credencial en claro"
			}
		}
	}
	return ""
}

// esPlantilla reporta si el archivo es una plantilla de configuración
// (.env.example, config.sample): existe para llevar valores de relleno, y
// el arquitecto está obligado a escribir un .env.example. Una clave real
// ahí la siguen cazando los patrones de proveedor.
func esPlantilla(archivo string) bool {
	switch path.Ext(strings.ToLower(archivo)) {
	case ".example", ".sample", ".template", ".dist":
		return true
	}
	return false
}

// esConfiguracion reporta si el archivo guarda valores sin comillas:
// .env, yaml, ini, toml, properties.
func esConfiguracion(archivo string) bool {
	base := strings.ToLower(path.Base(archivo))
	if strings.HasPrefix(base, ".env") || strings.HasSuffix(base, ".env") {
		return true
	}
	switch path.Ext(base) {
	case ".yml", ".yaml", ".ini", ".toml", ".properties", ".cfg", ".conf", ".env", "":
		return true
	}
	return false
}
