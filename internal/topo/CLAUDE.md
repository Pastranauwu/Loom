# internal/topo: advertencias

- **El propósito de un módulo sale del comentario del paquete o archivo (`Node.Doc`), del archivo menos profundo (`mejorDoc`):** determinista; nunca del orden del mapa.
- **`Detalle` es lo que el modelo lee para explicar un módulo (`loom map <módulo>`, `loom_map` con `module`):** debe bastar para responder sin abrir código. Si agregas un dato, que sea del grafo o de los contratos, no de leer archivos.
