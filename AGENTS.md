# AGENTS.md — pgflow

Guía para que un agente entienda y modifique este repo rápido. Léela antes de tocar código.

## Qué es

`pgflow` es una **TUI en Go** (terminal) para **backup y restore de PostgreSQL**.
Construida con **Bubble Tea** (`charmbracelet/bubbletea` + `bubbles` + `lipgloss`).
Un solo binario, sin runtime. Reemplaza a la versión bash original (`pgflow.sh`, aún en el repo como referencia).

Módulo: `github.com/ander0code/pgflow` · Go 1.25 · binario: `pgflow`.

## Modelo mental (el flujo) — clave para no confundir las acciones

```
   PRODUCCIÓN              archivo .dump                 BASE LOCAL
   (vía túnel SSH)  ──b──▶  (tu respaldo, en disco) ──r──▶  NUEVA (o reemplaza)
```

- **`b` backup**  = trae una base de **producción** (por túnel SSH) y la guarda como `<db>_<ts>.dump`. Es el respaldo.
- **`r` restore** = toma un `.dump` y lo carga en una base **local nueva** (o reemplaza una existente con DROP+CREATE).
- `v` verify · `d` delete · `t` túnel SSH · `c` config · `?` ayuda · `q` salir.

`pgflow` nunca escribe en producción: solo lee (dump). El restore solo toca la base **local**.

## Quickstart (comandos)

```sh
go build -o pgflow .        # o: make build
go run .                    # o: make run   (lanza la TUI — necesita TTY)
go test ./...               # o: make test
go vet ./...
gofmt -l .                  # debe salir vacío
go run honnef.co/go/tools/cmd/staticcheck@latest ./...   # debe salir sin hallazgos
bash install.sh             # compila + instala en ~/.local/bin/pgflow
make build-all              # cross-compile darwin/linux × amd64/arm64 → dist/

# CLI sin TUI:
pgflow --list [--json]      # lista backups
pgflow --version | --help

# Inspeccionar el render real sin TTY (ANSI quitado):
go test ./internal/tui/ -run TestSnapshot -v
```

**Definition of done** para cualquier cambio: `go build` ✓ · `go vet` ✓ · `gofmt -l` vacío ✓ · `staticcheck` sin hallazgos ✓ · `go test ./...` ✓.

## Arquitectura (capas)

| Paquete | Responsabilidad |
|---|---|
| `internal/config` | Lee/escribe `~/.pgflow.conf` (formato shell `KEY="value"`). Tipos `Config`, `Conn`. |
| `internal/pg` | Envuelve `psql`/`pg_dump`/`pg_restore`. Dump/restore en streaming, verify, traducción de errores. |
| `internal/tunnel` | Túnel SSH a producción: `IsUp` (dial TCP), `Ensure` (`ssh -f -N`), `Close` (`pkill`). |
| `internal/backups` | Escanea el directorio de backups → `[]Folder` con `[]Dump` (tamaño, fecha, validez). |
| `internal/tui` | La app Bubble Tea (Model/Update/View, estilos, comandos async, mensajes). |
| `main.go` | Flags CLI + arranque del programa (`tea.NewProgram(..., tea.WithAltScreen())`). |

Dependencias (no cycles): `tui → {config, pg, tunnel, backups}` · `backups → pg` · `pg → config` · `tunnel → config`.

## Layout

```
main.go                      flags (--list/--json/--version/--help) + arranque TUI
internal/config/config.go    Config{Local,Prod Conn; ProdSSH, ProdRemotePort, BackupDir, MinDiskMB}
internal/pg/pg.go            TestConn, ListDatabases, DatabaseExists, Create/RecreateDatabase,
                             DumpStream, RestoreStream, Verify, CountTables, classify*Error
internal/tunnel/tunnel.go    IsUp, Ensure, Close
internal/backups/backups.go  Folder{Name,Path,Dumps}, Dump{Name,Path,SizeBytes,ModTime,Valid,Objects}; Scan, Total
internal/tui/model.go        Model, Init, Update, key handling, wizard/flow logic
internal/tui/render.go       View y todas las funciones render*
internal/tui/styles.go       paleta (Catppuccin Mocha + morado Charm) y estilos lipgloss
internal/tui/messages.go     tipos tea.Msg
internal/tui/commands.go     funciones que devuelven tea.Cmd (trabajo async)
internal/tui/render_smoke_test.go  tests: renderiza todas las pantallas (no panics) + snapshots
pgflow.sh                    versión bash original (legacy, referencia)
pgflow.conf.example          plantilla de config (se commitea; sin credenciales)
Makefile install.sh uninstall.sh
```

## Arquitectura de la TUI (Bubble Tea)

`Model` (en `model.go`) guarda **todo el estado**. Patrón estándar: `Init` lanza comandos iniciales,
`Update(msg)` muta `*Model` y devuelve comandos, `View()` compone la pantalla.

**Pantallas** (`m.scr`): `screenDashboard`, `screenBackup`, `screenRestore`, `screenConfig`.
**Modales** (`m.modal`): `modalNone`, `modalConfirmDelete`, `modalError`, `modalHelp`.
**Input compartido** (`m.inputPurpose`): `inpNewFolder`, `inpNewDBName`, `inpConfigField` (usa `bubbles/textinput`).

**Asistentes = máquina de estados por `m.step`:**
- Backup: `1` elegir base → `2` elegir/crear carpeta → `3` confirmar → stream.
- Restore: `1` carpeta → `2` dump → `3` destino (crear/reemplazar) → `31` elegir base local (si reemplazar) → `4` confirmar → stream.
- `←`/`h` retrocede un paso; `esc` cancela. Las listas usan el tipo `picker`.

**Comandos ↔ mensajes** (`commands.go` ↔ `messages.go`):

| Comando | Mensaje | Hace |
|---|---|---|
| `scanCmd` | `scanDoneMsg` | escanea el dir de backups |
| `statusCmd` | `statusDoneMsg` | prueba local/túnel/prod (badges) |
| `prepBackupCmd` | `prodDBsMsg` | abre túnel + lista bases de prod |
| `localDBsCmd` | `localDBsMsg` | lista bases locales |
| `streamDumpCmd` / `streamRestoreCmd` | `streamStartedMsg` → `logEventMsg` | dump/restore con logs en vivo |
| `verifyCmd`/`deleteCmd`/`tunnelToggleCmd`/`testConnCmd` | `verifyDoneMsg`/`deleteDoneMsg`/`tunnelDoneMsg`/`connTestMsg` | acciones |
| `tickCmd`/`spinnerTickCmd`/`splashTickCmd`/`statusClearCmd` | timers | refresco, spinner, splash, auto-limpiar status |

**Logs en tiempo real (patrón importante):** `streamDumpCmd` crea un `chan logEvent` con buffer,
lanza una goroutine que corre `pg.DumpStream(..., onLine)` (cada línea de stderr de `pg_dump --verbose`
se manda al canal), y termina con `logEvent{done:true, ...}`. Devuelve `streamStartedMsg{ch}`. El modelo
guarda el canal y emite `waitForLog(ch)`, que **bloquea en `<-ch`** y devuelve `logEventMsg`. En cada
evento no-final el modelo vuelve a emitir `waitForLog`; en el final corta. La pantalla de ejecución
(`renderRunScreen`) muestra la cola de líneas + spinner + tiempo.

## Convenciones y gotchas (LEER antes de tocar `render.go`)

- **lipgloss + fondo de modal:** NO pongas `Background()` en las cajas de modal. El texto interno no
  hereda el fondo del padre → aparecen bloques oscuros disparejos. Los modales van **sin fondo** (solo
  borde). Mismo motivo: evita anidar estilos con fondo.
- **Filas seleccionadas:** construye texto plano + `padLine(linea, ancho)` y envuélvelo en
  `rowSelectedStyle`. Para filas NO seleccionadas, concatena segmentos estilados por separado (evita
  ANSI anidado que rompe el reset).
- **No al wrap:** los valores largos se **truncan** (`truncate`); los paths se cortan por la izquierda
  (`truncateLeft`, conserva la cola). Ancho de modales: `m.modalWidth()` / `m.helpWidth()` (se adaptan).
- **Sin código muerto:** `staticcheck` (U1000) marca vars/funcs/campos a nivel de paquete sin usar. No
  dejes estilos ni helpers sin usar. Los errores no deben terminar en punto/newline (ST1005).
- **Errores "humanos":** `pg/pg.go` traduce stderr a mensajes accionables (`classify*Error`). El caso de
  permisos parsea el objeto denegado y sugiere `GRANT pg_read_all_data`. El modal de error es multilínea.
- **Estilo de marca:** el tema (paleta Catppuccin Mocha + morado Charm `#7D56F4`) replica al proyecto
  hermano **lazyports**. Mantén la coherencia visual.

## Configuración

`~/.pgflow.conf` (formato shell `KEY="value"`; **está en `.gitignore`**, nunca se commitea). Campos:

```
PGFLOW_LOCAL_HOST/PORT/USER/PASS         # PostgreSQL local (destino del restore)
PGFLOW_PROD_SSH                          # alias de ~/.ssh/config con LocalForward
PGFLOW_PROD_HOST/PORT/REMOTE_PORT/USER/PASS   # prod a través del túnel (origen del backup)
PGFLOW_BACKUP_DIR                        # dónde se guardan los .dump
PGFLOW_MIN_DISK_MB
```

Las contraseñas pueden ir vacías y usar `~/.pgpass`. Plantilla: `pgflow.conf.example`.

## Notas operativas

- **Plataformas soportadas:** macOS, Linux y **Windows 10 1809+** (donde `ssh.exe`
  viene integrado). `internal/tunnel` corre ssh como subprocess de Go (sin
  `ssh -f` ni `pkill`) y `tunnel_windows.go` setea `CREATE_NO_WINDOW` para
  evitar el flash de consola. Build tags separados: `tunnel_unix.go`
  (`//go:build !windows`) y `tunnel_windows.go` (`//go:build windows`).
- **Permisos (causa de fallo #1 en backup):** `pg_dump` corre con el rol configurado y hace
  `LOCK TABLE … IN ACCESS SHARE MODE` sobre **todas** las tablas → el rol necesita **lectura total**.
  Solución: `GRANT pg_read_all_data TO <rol>;` (PostgreSQL 14+) o un rol superusuario. La app ya lo
  indica en el confirm y en el error.
- **Túnel:** se abre solo si el puerto local no responde (`net.DialTimeout`), vía `ssh -f -N <alias>`.
  El alias debe traer su `LocalForward` en `~/.ssh/config`.
- **Restore seguro:** `pg_restore --single-transaction` (todo o nada). El modo *reemplazar* (DROP+CREATE)
  se marca en rojo (modal de borde doble) por ser irreversible.

## Testing

`internal/tui/render_smoke_test.go`:
- `TestViewAcrossStates` — recorre todas las pantallas/modales y verifica que `View()` no entra en pánico
  ni devuelve vacío. **Añade aquí** cualquier pantalla nueva.
- `TestSnapshot` — renderiza pantallas con ANSI quitado y las imprime con `t.Logf` (úsalo con `-v` para
  inspeccionar el layout sin TTY).
- Fixtures con **nombres genéricos** (`tienda-web`, `shopdb`, `produser`) — NO uses datos reales de
  clientes/infra (el repo es público).

## Cómo extender (recetas)

- **Nuevo campo de config:** agrégalo a `Config`/`Save`/`Load` (`config.go`) y a `buildCfgFields`
  (`model.go`, con su `section`).
- **Nueva acción de dashboard:** añade un `case` en `handleDashboardKey` (`model.go`) y, si es async,
  un comando en `commands.go` + un mensaje en `messages.go` + su `case` en `Update`.
- **Nuevo paso de asistente:** ajusta la máquina `m.step` en `*Select`/`*Back` y `crumb()`.
- **Nuevo modal:** añade la constante al enum `modal`, manéjalo en `handleModalKey` y `renderModal`.
- Tras cualquier cambio de UI, corre el snapshot test para verlo.

## Limitaciones conocidas

- **Sin scroll** en listas largas: si una carpeta tiene más dumps de los que caben en el panel, los de
  abajo se recortan (el cursor se mueve pero la vista no lo sigue). Pendiente: viewport con scroll
  (`bubbles/viewport`) en `renderBackupList` y `renderRunScreen`.

## Atajos (referencia)

- **Dashboard:** `b` backup · `r` restore · `v` verify · `d` delete · `t` túnel · `c` config · `?` ayuda · `q` salir · `j/k`,`↑/↓` navegar · `g/G` primero/último · `enter` abrir/restaurar o contraer carpeta · `^r`/`R` refrescar.
- **Asistentes:** `↑/↓`,`j/k` elegir · `enter` ok · `←`/`h` atrás · `esc` cancelar.
- **Config:** `↑/↓` campo · `enter` editar · `l`/`p` probar local/prod · `esc` volver.
