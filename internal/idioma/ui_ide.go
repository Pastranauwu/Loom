package idioma

func init() {
	for es, en := range ide {
		textos[es] = en
	}
}

var ide = map[string]string{
	"falta tmux · instálalo (pacman -S tmux, apt install tmux, brew install tmux) y repite loom ide": "tmux is missing · install it (pacman -S tmux, apt install tmux, brew install tmux) and run loom ide again",
	"falta claude · instala Claude Code y repite loom ide":                                           "claude is missing · install Claude Code and run loom ide again",
	"ocultar": "hide",
	"volver":  "back",
	"no instalado · loom ide no va a poder abrirse":                                           "not installed · loom ide will not open",
	"· loom ide abre el espacio de trabajo: Claude conectado a loom, tablero, mapa y consumo": "· loom ide opens the workspace: Claude wired to loom, board, map and usage",
}
