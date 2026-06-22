# AGENTS.md — pgflow

Guía para que un agente entienda y modifique este repo rápido. Léela antes de tocar código.

## Qué es

`pgflow` es una **TUI en Go** (terminal) para **backup y restore de PostgreSQL**.
Construida con **Bubble Tea** (`charmbracelet/bubbletea` + `bubbles` + `lipgloss`).
Un solo binario, sin runtime, multiplataforma (macOS · Linux · Windows). Reemplaza a la
versión bash original (`pgflow.sh`, aún en el repo como referencia).

Módulo: `github.com/ander0code/pgflow` · Go 1.25 · binario: `pgflow`.

## Modelo mental (el flujo) — clave para no confundir las acciones

```
   PRODUCCIÓN              archivo .dump                 BASE LOCAL
   (vía túnel SSH)  ──b──▶  (tu respaldo, en disco) ──r──▶  NUEVA (o reemplaza)
```

- **`b` backup**  = trae una base de **producción** (por túnel SSH) y la guarda como `.dump`. El nombre lo arma `naming.Render(tmpl, db, prefix, now, seq)` con la **plantilla por db** (default `{prefix}-{db}-{datetime}` o `{db}_{datetime}`); en el paso Confirmar se puede editar el nombre (`e`), la plantilla (`t`) y el prefijo de carpeta (`p`).
- **`r` restore** = toma un `.dump` y lo carga en una base **local nueva** (o reemplaza una existente con DROP+CREATE).
- `v` verify · `d` delete · `t` túnel SSH · `c` config · `?` ayuda · `q` salir.

`pgflow` nunca escribe en producción: solo lee (dump). El restore solo toca la base **local**.

## Cómo correrlo / instalar

```sh
# Instalar (binario prebuilt, NO necesita Go):
#   macOS / Linux:
curl -fsSL https://raw.githubusercontent.com/ander0code/pgflow/main/install.sh | bash
#   Windows (PowerShell):
#   irm https://raw.githubusercontent.com/ander0code/pgflow/main/install.ps1 | iex

# Ejecutar:
pgflow                 # abre la TUI (necesita TTY)
pgflow --list          # lista backups (texto)
pgflow --list --json   # lista backups (JSON, para pipe/jq)
pgflow --version | --help

# Desinstalar:
bash uninstall.sh      # (Windows: .\uninstall.ps1)
```

## Quickstart de desarrollo (comandos)

```sh
go build -o pgflow .        # o: make build
go run .                    # o: make run   (lanza la TUI — necesita TTY)
go test -race ./...         # o: make test
go vet ./...
gofmt -l .                  # debe salir vacío
go run honnef.co/go/tools/cmd/staticcheck@latest ./...   # debe salir sin hallazgos
make build-all              # cross-compile darwin/linux/windows × amd64/arm64 → dist/

# Inspeccionar el render real sin TTY (ANSI quitado):
go test ./internal/tui/ -run TestSnapshot -v
```

**Definition of done** para cualquier cambio: `go build ./...` ✓ · `go vet ./...` ✓ ·
`gofmt -l .` vacío ✓ · `staticcheck` sin hallazgos ✓ · `go test -race ./...` ✓.
El CI (`.github/workflows/ci.yml`) corre exactamente esto en ubuntu/macos/windows + cross-build.

## Arquitectura (capas)

| Paquete | Responsabilidad |
|---|---|
| `internal/config` | Lee/escribe `~/.pgflow.conf` (formato shell `KEY="value"`). Tipos `Config`, `Conn`. |
| `internal/pg` | Envuelve `psql`/`pg_dump`/`pg_restore`. Dump/restore en streaming, verify, traducción de errores. |
| `internal/tunnel` | Túnel SSH a producción: `IsUp` (dial TCP), `Ensure` (`ssh -N` como subproceso Go, con alias validado y reap automático en caso de fallo), `Close`/`IsOpen` (gestiona el PID, sin `ssh -f`/`pkill`). |
| `internal/backups` | Escanea el directorio de backups → `[]Folder` con `[]Dump` (tamaño, fecha, validez). |
| `internal/tui` | La app Bubble Tea (Model/Update/View, estilos, comandos async, mensajes). |
| `main.go` | Flags CLI + arranque del programa (`tea.NewProgram(..., tea.WithAltScreen())`). |

Dependencias (no cycles): `tui → {config, pg, tunnel, backups}` · `backups → pg` · `pg → config` · `tunnel → config`.

## Layout

```
main.go                      flags (--list/--json/--version/--help) + arranque TUI
internal/config/config.go    Config{Local,Prod Conn; ProdSSH, ProdRemotePort, BackupDir, MinDiskMB}
internal/pg/pg.go            TestConn, ListDatabases, Create/RecreateDatabase,
                             DumpStream, RestoreStream, Verify, CountTables, classify*Error,
                             ValidHost/ValidPort/ValidIdent (allowlists)
internal/tunnel/tunnel.go    IsUp, Ensure, Close, IsOpen, ValidAlias (núcleo, multiplataforma)
internal/tunnel/tunnel_unix.go     hideWindow no-op   (//go:build !windows)
internal/tunnel/tunnel_windows.go  hideWindow → CREATE_NO_WINDOW (//go:build windows)
internal/tunnel/tunnel_test.go     test del ciclo abrir/cerrar (inyecta un ssh falso)
internal/backups/backups.go  Folder{Name,Path,Dumps}, Dump{Name,Path,SizeBytes,ModTime,Valid,Objects}; Scan, Total
internal/naming/naming.go    ~/.pgflow.json: prefijo por carpeta + plantilla y secuencia por db. Prefix/SetPrefix, Template/SetTemplate, Seq/BumpSeq, Render, DefaultTemplate, Tokens
internal/tui/model.go        Model, Init, Update, key handling, wizard/flow logic
internal/tui/render.go       View y todas las funciones render*
internal/tui/styles.go       paleta (Catppuccin Mocha + morado Charm) y estilos lipgloss
internal/tui/messages.go     tipos tea.Msg
internal/tui/commands.go     funciones que devuelven tea.Cmd (trabajo async)
internal/tui/render_smoke_test.go  tests: renderiza todas las pantallas (no panics) + snapshots
pgflow.sh                    versión bash original (legacy, referencia)
pgflow.conf.example          plantilla de config (se commitea; sin credenciales)
Makefile                     build / test / install / build-all (cross-compile)
install.sh / install.ps1     instaladores (descargan el release; fallback a compilar)
uninstall.sh / uninstall.ps1 desinstaladores
.github/workflows/ci.yml     CI: build+vet+gofmt+test en linux/macos/windows + cross-build
.gitattributes               fuerza EOL=LF (gofmt-safe en runners Windows)
```

## Arquitectura de la TUI (Bubble Tea)

`Model` (en `model.go`) guarda **todo el estado**. Patrón estándar: `Init` lanza comandos iniciales,
`Update(msg)` muta `*Model` y devuelve comandos, `View()` compone la pantalla.

**Pantallas** (`m.scr`): `screenDashboard`, `screenBackup`, `screenRestore`, `screenConfig`.
**Modales** (`m.modal`): `modalNone`, `modalConfirmDelete`, `modalError`, `modalHelp`.
**Input compartido** (`m.inputPurpose`): `inpNewFolder`, `inpNewDBName`, `inpConfigField`, `inpDumpName`, `inpFolderPrefix`, `inpTemplate` (usa `bubbles/textinput`).

**Nombre del dump (paso Confirmar del backup):** `proposeDumpName()` arma el nombre con la **plantilla por db** (`naming.Template`, default `naming.DefaultTemplate(prefijo)`) vía `naming.Render(tmpl, db, prefix, now, seq)`. Tokens: `{db} {date} {time} {datetime} {seq} {prefix}`. El footer del confirm (`backupConfirmKeys`) expone: `e` editar nombre (`inpDumpName`), `t` editar plantilla por db (`inpTemplate`), `p` prefijo de la carpeta (`inpFolderPrefix`). Si la plantilla usa `{seq}`, el contador (`naming.Seq`) se **autoincrementa** (`naming.BumpSeq`) al terminar el backup OK. Todo se guarda en `~/.pgflow.json`. El nombre elegido (`m.bkFile`) se pasa a `streamDumpCmd`.

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
- **Cross-platform:** nada de `pkill`/`ssh -f` ni paths Unix hardcodeados. El código corre en Windows;
  lo específico de SO va en archivos con build tags (`*_unix.go` / `*_windows.go`).
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

El **naming** vive aparte en `~/.pgflow.json` (`{"prefixes":{"<carpeta>":"…"}, "templates":{"<db>":"…"}, "seq":{"<db>":N}}`),
gestionado por `internal/naming`. Está en `$HOME`, nunca se commitea.

## Notas operativas

- **Plataformas:** macOS, Linux y **Windows 10 1809+** (donde `ssh.exe` viene integrado).
- **Túnel:** se abre solo si el puerto local no responde (`net.DialTimeout`), corriendo `ssh -N <alias>`
  como **subproceso de Go** (sin `ssh -f` ni `pkill`); `Close` mata por PID. El alias debe traer su
  `LocalForward` en `~/.ssh/config`. En Windows `tunnel_windows.go` setea `CREATE_NO_WINDOW` para que no
  parpadee una consola.
- **Permisos (causa de fallo #1 en backup):** `pg_dump` corre con el rol configurado y hace
  `LOCK TABLE … IN ACCESS SHARE MODE` sobre **todas** las tablas → el rol necesita **lectura total**.
  Solución: `GRANT pg_read_all_data TO <rol>;` (PostgreSQL 14+) o un rol superusuario. La app ya lo
  indica en el confirm y en el error.
- **Restore seguro:** `pg_restore --single-transaction` (todo o nada). El modo *reemplazar* (DROP+CREATE)
  se marca en rojo (modal de borde doble) por ser irreversible.

## Release / distribución

- `make build-all` cross-compila a `dist/pgflow-<os>-<arch>[.exe]` (6 binarios).
- Release con GitHub CLI: `gh release create vX.Y.Z dist/pgflow-* dist/checksums.txt --title ... --notes ...`.
- Los instaladores descargan de `releases/latest/download/pgflow-<os>-<arch>` (siempre el último release),
  así que el one-liner `curl|bash` / `irm|iex` no hardcodea versión.
- Versión: el binario muestra `main.version`, inyectada vía `-ldflags "-X main.version=$(git describe --tags)"`
  (el Makefile lo hace). Sin tag, sale `dev`.

## Testing

- `internal/tui/render_smoke_test.go`:
  - `TestViewAcrossStates` — recorre todas las pantallas/modales y verifica que `View()` no entra en
    pánico ni devuelve vacío. **Añade aquí** cualquier pantalla nueva.
  - `TestSnapshot` — renderiza pantallas con ANSI quitado y las imprime con `t.Logf` (úsalo con `-v`
    para inspeccionar el layout sin TTY).
- `internal/tunnel/tunnel_test.go` — prueba el ciclo abrir/cerrar inyectando un `ssh` falso vía la
  variable `newSSHCommand` (no abre conexiones reales).
- Fixtures con **nombres genéricos** (`tienda-web`, `shopdb`, `produser`) — NO uses datos reales de
  clientes/infra (el repo es público).

## Cómo extender (recetas)

- **Nuevo campo de config:** agrégalo a `Config`/`Save`/`Load` (`config.go`) y a `buildCfgFields`
  (`model.go`, con su `section`).
- **Nueva acción de dashboard:** añade un `case` en `handleDashboardKey` (`model.go`) y, si es async,
  un comando en `commands.go` + un mensaje en `messages.go` + su `case` en `Update`.
- **Nuevo paso de asistente:** ajusta la máquina `m.step` en `*Select`/`*Back` y `crumb()`.
- **Nuevo modal:** añade la constante al enum `modal`, manéjalo en `handleModalKey` y `renderModal`.
- **Algo específico de SO:** ponlo en `*_unix.go` / `*_windows.go` con build tags; nunca shell-outs no
  portables en el código común.
- Tras cualquier cambio de UI, corre el snapshot test para verlo.

## Limitaciones conocidas

- **Sin scroll** en listas largas: si una carpeta tiene más dumps de los que caben en el panel, los de
  abajo se recortan (el cursor se mueve pero la vista no lo sigue). Pendiente: viewport con scroll
  (`bubbles/viewport`) en `renderBackupList` y `renderRunScreen`.

## Atajos (referencia)

- **Dashboard:** `b` backup · `r` restore · `v` verify · `d` delete · `t` túnel · `c` config · `?` ayuda · `q` salir · `j/k`,`↑/↓` navegar · `g/G` primero/último · `enter` abrir/restaurar o contraer carpeta · `^r`/`R` refrescar.
- **Asistentes:** `↑/↓`,`j/k` elegir · `enter` ok · `←`/`h` atrás · `esc` cancelar.
- **Config:** `↑/↓` campo · `enter` editar · `l`/`p` probar local/prod · `esc` volver.
