package idioma

func init() {
	for es, en := range ide {
		textos[es] = en
	}
}

var ide = map[string]string{
	"✓ claude conectado a loom por MCP (herramientas loom_*)":                              "✓ claude wired to loom over MCP (loom_* tools)",
	"· no se pudo conectar claude a loom · %s · hazlo con claude mcp add loom -- loom mcp": "· could not wire claude to loom · %s · do it with claude mcp add loom -- loom mcp",
	"uso": "usage", "tareas": "tasks", "mapa": "map", "tareas · mapa": "tasks · map",
	"tab cambia · j/k mueve · enter detalle · esc atrás":                                             "tab switches · j/k moves · enter detail · esc back",
	"tab cambia · j/k desplaza · g/G inicio/fin":                                                     "tab switches · j/k scrolls · g/G top/bottom",
	"sin tareas · pídele a Claude un spec":                                                           "no tasks · ask Claude for a spec",
	"falta tmux · instálalo (pacman -S tmux, apt install tmux, brew install tmux) y repite loom ide": "tmux is missing · install it (pacman -S tmux, apt install tmux, brew install tmux) and run loom ide again",
	"falta claude · instala Claude Code y repite loom ide":                                           "claude is missing · install Claude Code and run loom ide again",
	"ocultar": "hide",
	"volver":  "back",
	"no instalado · loom ide no va a poder abrirse":                                           "not installed · loom ide will not open",
	"· loom ide abre el espacio de trabajo: Claude conectado a loom, tablero, mapa y consumo": "· loom ide opens the workspace: Claude wired to loom, board, map and usage",
}
