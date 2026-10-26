# Tape de vhs para grabar el GIF de loom CON la TUI.
#   vhs docs/demo.tape
#
# Graba run (corrida en vivo), board (tablero), la recursion en subtareas
# y ship (compuerta), sin --plain.
#
# La preparacion va entre Hide/Show: compilar, entrar al repo de demo y
# exportar el entorno no le dicen nada a quien mira el GIF, y ocupaban los
# primeros segundos del bucle.
#
# HOME apunta al home falso que crea demo-env.sh: el ledger de consumo vive
# en $HOME/.loom y sin aislarlo el GIF publica el gasto real de quien
# graba.
#
# Las esperas son Wait+Screen sobre lo que el comando imprime, no Sleep a
# ojo: un Sleep corto cortaba la corrida a la mitad en una maquina lenta y
# uno largo dejaba segundos muertos. Cada seccion arranca con un clear
# oculto para que el patron de Wait no matchee la salida de la anterior.
#
# OJO: en vhs, Type manda los caracteres literal (no escapa $). Escribir
# \$PATH rompe el PATH. Aqui va $PATH tal cual.
# En vhs, Width y Height son PIXELES (tamano de la imagen final).

Output docs/demo.gif

Set Shell bash
Set FontSize 20
Set Width 1200
Set Height 700
Set Padding 20
Set Framerate 24
Set TypingSpeed 40ms
Set WaitTimeout 180s

# ---------- preparacion: fuera del GIF ----------
Hide
Type "sh scripts/demo-env.sh"
Enter
Wait+Screen /demo lista/
Type "cd /tmp/loom-demo"
Enter
Type "export PATH=/tmp/fakebin:$PATH"
Enter
Type "export HOME=/tmp/loom-demo-home OPENCODE_API_KEY=demo"
Enter
Type "clear"
Enter
Show

# ---------- run: la corrida en vivo, sale sola ----------
Type "loom run --agentes 2 --ejecutor opencode"
Enter
Wait+Screen /verde en/
Sleep 2s

Hide
Type "clear"
Enter
Show

# ---------- board: el tablero ----------
Type "loom board"
Enter
Wait+Screen /LISTO PARA ENTREGAR/
Sleep 4s
Type "q"
Sleep 1s

# ---------- recursion: T-003 se reparte sola en subtareas ----------
# el mv es plomeria de la demo (ver scripts/demo-env.sh): T-003 llega
# como .pendiente porque entrando desde el arranque la esclusa la rechaza
# por cruce de tocar_solo con T-001/T-002 en la misma oleada, que
# es un chequeo real y correcto. Va oculto: no es parte de lo que se
# muestra.
Hide
Type "clear"
Enter
Type "mv .loom/tasks/T-003.md.pendiente .loom/tasks/T-003.md"
Enter
Type "clear"
Enter
Show

Type "loom run --agentes 1 --ejecutor opencode"
Enter
Wait+Screen /verde en/
Sleep 2s

Hide
Type "clear"
Enter
Show

# board --plain a proposito aqui (no TUI): entrar y salir de un segundo
# alt-screen en la misma sesion de vhs es fragil (la tecla "q" puede
# llegarle a la shell en vez de al programa si el timing no da), y el
# texto plano muestra el arbol de subtareas de T-003 con mas claridad
# para un GIF de todos modos.
Type "loom board --plain"
Enter
Sleep 4s

Hide
Type "clear"
Enter
Show

# ---------- ship: la compuerta, sale sola ----------
Type "loom ship T-001 --dry-run"
Enter
Sleep 6s
