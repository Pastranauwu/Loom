package idioma

func init() {
	for es, en := range wrapTextos {
		textos[es] = en
	}
}

var wrapTextos = map[string]string{
	"· el spec declara autenticación · agrega a mano los encabezados al puente":            "· the spec declares authentication · add the headers to the bridge by hand",
	"✓ puente %s · %d herramientas: %s":                                                    "✓ bridge %s · %d tools: %s",
	"puente %s · %d herramientas: %s":                                                      "bridge %s · %d tools: %s",
	"✓ puente %s borrado":                                                                  "✓ bridge %s removed",
	"· %s · el campo %s es %s y se omite del cuerpo":                                       "· %s · field %s is %s and is left out of the body",
	"· %s existe pero no respondió a --version ni --help":                                  "· %s exists but answered neither --version nor --help",
	"· sin puentes · loom wrap --cli <binario>":                                            "· no bridges · loom wrap --cli <binary>",
	"· solo se envolvieron las primeras %d operaciones":                                    "· only the first %d operations were wrapped",
	"el argumento %s debe ser %v":                                                          "argument %s must be %v",
	"el cURL no trae url · pega un comando completo":                                       "the cURL has no url · paste a complete command",
	"el cURL tiene una comilla sin cerrar":                                                 "the cURL has an unclosed quote",
	"el encabezado %s trae un salto de línea":                                              "header %s contains a line break",
	"el HAR no trae llamadas JSON que envolver":                                            "the HAR has no JSON calls to wrap",
	"el manifiesto no es JSON válido · %s":                                                 "the manifest is not valid JSON · %s",
	"el modelo no devolvió un manifiesto · vuelve a intentarlo":                            "the model returned no manifest · try again",
	"el puente no trae herramientas · vuelve a intentarlo":                                 "the bridge has no tools · try again",
	"el spec no declara un servidor absoluto · agrega servers con una url http(s)":         "the spec declares no absolute server · add servers with an http(s) url",
	"falta el argumento %s":                                                                "missing argument %s",
	"falta el modelo de la síntesis · revisa cli en .loom/config.yml":                      "no model for the synthesis · check cli in .loom/config.yml",
	"herramienta %q con nombre inválido · usa minúsculas, dígitos y _":                     "tool %q has an invalid name · use lowercase, digits and _",
	"herramienta %q repetida":                                                              "tool %q repeated",
	"herramienta %s · bin %q no es un nombre de binario simple":                            "tool %s · bin %q is not a plain binary name",
	"herramienta %s · debe traer exec o http, uno solo":                                    "tool %s · needs exec or http, exactly one",
	"herramienta %s · inputSchema debe ser de tipo object":                                 "tool %s · inputSchema must be of type object",
	"herramienta %s · la url debe empezar con http:// o https://":                          "tool %s · the url must start with http:// or https://",
	"herramienta %s · la variable {%s} debe estar en required":                             "tool %s · variable {%s} must be in required",
	"herramienta %s · la variable {%s} no es propiedad del inputSchema":                    "tool %s · variable {%s} is not an inputSchema property",
	"herramienta %s · método %q no permitido":                                              "tool %s · method %q not allowed",
	"herramienta %s · %s ejecuta texto como programa · los puentes no pasan por una shell": "tool %s · %s runs text as a program · bridges never go through a shell",
	".loom/bridges/%s.json no es JSON válido · %w":                                         ".loom/bridges/%s.json is not valid JSON · %w",
	"\n--dry-run · no se guardó el puente":                                                 "\n--dry-run · the bridge was not saved",
	"no existe el puente %s · mira loom wrap list":                                         "no bridge %s · see loom wrap list",
	"nombre de puente inválido: %q · usa minúsculas, dígitos y _":                          "invalid bridge name: %q · use lowercase, digits and _",
	"pasa una sola fuente · --cli, --curl, --har o --openapi":                              "pass exactly one source · --cli, --curl, --har or --openapi",
	"puente rechazado · %v":                                                                "bridge rejected · %v",
	"%q no es un binario que se pueda envolver":                                            "%q is not a binary that can be wrapped",
	"%s no devolvió ayuda con --help ni man":                                               "%s gave no help with --help or man",
	"%s no es un HAR válido · %w":                                                          "%s is not a valid HAR · %w",
	"%s no es un spec OpenAPI con paths":                                                   "%s is not an OpenAPI spec with paths",
	"%s no está en el PATH · instálalo y reintenta":                                        "%s is not on the PATH · install it and retry",
	"%s respondió %s":               "%s answered %s",
	"wrap · sintetizando el puente": "wrap · synthesizing the bridge",
}
