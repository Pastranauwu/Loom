package main

import (
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/graph"
)

func TestDetallesRelevantesDaElMapaDelModuloDelPedido(t *testing.T) {
	g := &graph.Graph{Files: map[string]graph.Node{
		"tienda/servicios/pedidos.py":  {Doc: "arma y envía pedidos", Symbols: []string{"def enviar_pedido(p)"}},
		"tienda/tests/test_pedidos.py": {Imports: []string{"tienda/servicios/pedidos"}},
		"pagos/models.py":              {Symbols: []string{"class Cobro"}},
	}}
	g.Files["tienda/servicios/pedidos.py"] = graph.Node{Doc: "arma y envía pedidos", Symbols: []string{"def enviar_pedido(p)"}, ImportedBy: []string{"tienda/tests/test_pedidos.py"}}
	d := detallesRelevantes(g, "el envío de pedidos falla cuando el correo está caído")
	if !strings.Contains(d, "Módulo tienda/servicios") || !strings.Contains(d, "def enviar_pedido") || strings.Contains(d, "pagos") {
		t.Errorf("solo el módulo del pedido, con su API:\n%s", d)
	}
	if detallesRelevantes(g, "xyz") != "" {
		t.Error("sin coincidencias no hay detalle: los más importados no son el pedido")
	}
}
