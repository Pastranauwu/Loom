package main

import (
	"slices"
	"strings"
	"testing"
)

// sinFondo es lo único que separa "desprenderse una vez" de "relanzarse
// para siempre": si la bandera sobrevive al filtro, el hijo vuelve a
// desprenderse, y su hijo también.
func TestSinFondoQuitaLaBanderaYNadaMas(t *testing.T) {
	casos := []struct {
		nombre string
		args   []string
		quiero []string
	}{
		{
			"bandera suelta",
			[]string{"run", "--agentes", "3", "--fondo", "--plain"},
			[]string{"run", "--agentes", "3", "--plain"},
		},
		{
			"bandera con valor, la otra forma que acepta cobra",
			[]string{"run", "--fondo=true", "--reintentar"},
			[]string{"run", "--reintentar"},
		},
		{
			"sin bandera, todo intacto",
			[]string{"up", "arregla el login", "--ship"},
			[]string{"up", "arregla el login", "--ship"},
		},
		{
			"no se lleva banderas que solo empiezan parecido",
			[]string{"run", "--fondo-falso", "--fondos"},
			[]string{"run", "--fondo-falso", "--fondos"},
		},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			got := sinFondo(c.args)
			if !slices.Equal(got, c.quiero) {
				t.Errorf("sinFondo(%v) = %v, quiero %v", c.args, got, c.quiero)
			}
			for _, a := range got {
				if a == FlagFondo || strings.HasPrefix(a, FlagFondo+"=") {
					t.Fatalf("la bandera sobrevivió: el hijo se relanzaría para siempre (%v)", got)
				}
			}
		})
	}
}

// El hijo tiene que arrancar en su propia sesión: sin eso hereda la
// terminal de control y el SIGHUP de cerrarla se lo lleva.
func TestDesprenderPideSesionPropia(t *testing.T) {
	if a := desprender(); a == nil {
		t.Fatal("desprender() devolvió nil · el hijo moriría con la terminal")
	}
}
