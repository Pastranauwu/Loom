package task

import "os"

// los tests fijan los mensajes en español; el inglés se prueba en internal/idioma
func init() { os.Setenv("LOOM_LANGUAGE", "es") }
