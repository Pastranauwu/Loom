# internal/idioma: advertencias

- **Los prompts y el JSON del modelo van en inglés; el español se sigue aceptando al leer:** etiquetas (`Cases:`/`Casos:`, `Obsolete:`/`Obsoleto:`) con `Tiene`/`Cortar`, llaves con `NormalizarClaves` (no pisa la canónica). Un consumidor nuevo de texto del modelo o de un contrato viejo acepta las dos.
- **Las marcas de notas y los encabezados de arquitectura viven en dos idiomas (`plan.SepararNotas`, `RecortarArquitectura`):** los contratos ya guardados traen el texto en español. Se escribe en inglés (`Spec rules:`, `FILE TREE`).
- **`language: es` solo agrega `Instruccion()` al final de los prompts cuya salida lee una persona:** planificador, arquitecto, revisor, visual, constitución. Nunca a los del implementador ni a los que devuelven JSON puro. Lo fija `config.Load`.
