package main

import (
	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/tui"
	"github.com/Pastranauwu/loom/internal/tutorial"
)

func newTutorialCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tutorial",
		Short: "a guided tour of loom for new developers",
		Long: `A short walkthrough of how loom works: spec, tasks, rooms, gates and the PR,
with the commands to try at each step. In a terminal it is paged (← →); with
--plain or a pipe it prints everything. It follows your language (loom language).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ps := tutorial.Paginas(idioma.Es())
			if !esTUI() {
				out.Line("%s", tutorial.Texto(ps))
				return out.Data(ps)
			}
			paginas := make([]tui.PaginaTutorial, len(ps))
			for i, p := range ps {
				paginas[i] = tui.PaginaTutorial{Titulo: p.Titulo, Cuerpo: p.Cuerpo}
			}
			return tui.Tutorial(paginas)
		},
	}
}
