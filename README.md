# 🐘 pgflow

> **Visual TUI for PostgreSQL backup & restore.** Dump a production database
> over an SSH tunnel and restore it into your local Postgres — all from a clean
> terminal UI, without memorizing `pg_dump` / `pg_restore` flags.
> Inspired by [lazygit](https://github.com/jesseduffield/lazygit) and built as a
> sibling to [lazyports](https://github.com/ander0code/lazyports).

```
 🐘 pgflow          local ✓ · prod ✓ · túnel abierto        25 backup(s) · 16:45:07
 ┌─ Backups (25) ─────────────────────────────┐┌─ Detalle ──────────────────┐
 │ ▾ tienda-web  (21)                          ││ shop_20260610_1645.dump     │
 │    ✓  shop_20260610_1645.dump   1.7MB       ││                             │
 │    ✓  shop_20260610_1135.dump   1.7MB       ││ carpeta  tienda-web         │
 │    ✗  shop_20260610_0801.dump      0B       ││ tamaño   1.7 MB             │
 │ ▸ blog  (3)                                 ││ objetos  1004               │
 │ ▾ api   (1)                                 ││ creado   2026-06-10 16:46   │
 │    ✓  api_20260601_0930.dump   4.2MB        ││ estado   ✓ válido           │
 └─────────────────────────────────────────────┘└─────────────────────────────┘
  b backup  r restore  v verify  d delete  t túnel  c config  ? ayuda  q quit
```

**Plataformas soportadas:** macOS · Linux · Windows 10 1809+ — **un solo binario, mismo código fuente**.

## Por qué pgflow

- **Un solo binario, cero runtime** — Go puro con [Bubble Tea](https://github.com/charmbracelet/bubbletea). No hay que instalar Python ni Node.
- **Flujos guiados** — backup (3 pasos) y restore (4 pasos) como asistentes con barra de progreso, resumen y confirmación. Puedes retroceder con `←`.
- **Túnel SSH automático** — abre el túnel a producción solo cuando hace falta (vía un alias de `~/.ssh/config` con `LocalForward`).
- **Integridad a la vista** — cada dump se verifica con `pg_restore --list`; los corruptos salen marcados con `✗`.
- **Restore seguro** — `--single-transaction` (todo o nada). El modo *reemplazar* (DROP + CREATE) avisa en rojo porque es irreversible.
- **Errores en cristiano** — túnel caído, credenciales, disco lleno, encoding incompatible, objetos duplicados… traducidos a mensajes accionables.
- **Modo CLI** — `pgflow --list [--json]` para scripting.

## Plataformas y compatibilidad

pgflow se compila desde el **mismo código fuente** para los tres sistemas operativos. La matriz de soporte:

| SO | Versión mínima | `ssh` | `psql` / `pg_dump` / `pg_restore` | Instalador | Arquitecturas |
|---|---|---|---|---|---|
| **macOS** | 10.15 (Catalina) | `brew install openssh` (o built-in) | `brew install postgresql` | `bash install.sh` | amd64 · arm64 (Apple Silicon) |
| **Linux** | glibc 2.31+ (Ubuntu 20.04 / Debian 11 / RHEL 8.4+) | `openssh-client` | `postgresql-client` | `bash install.sh` | amd64 · arm64 |
| **Windows** | 10 build 1809+ · 11 · Server 2019+ | **built-in** (OpenSSH client) | [postgresql.org/download/windows](https://www.postgresql.org/download/windows/) | `.\install.ps1` | amd64 · arm64 |

### Cómo se logra la portabilidad

- **Túnel SSH cross-platform.** `internal/tunnel` corre `ssh -N <alias>` como **subprocess de Go** (sin `ssh -f` ni `pkill`). Cierra por PID via `Process.Kill()`. Funciona idéntico en macOS, Linux y Windows — el `ssh.exe` que viene con Windows 10 1809+ respeta los mismos flags.
- **Build tags OS-específicos, sin condicionales dispersos.** Solo hay **un archivo** con código distinto por OS: `tunnel_windows.go` (8 líneas). El resto del código es portable sin condición.
  - `tunnel_unix.go` → `//go:build !windows` → compila en darwin + linux
  - `tunnel_windows.go` → `//go:build windows` → compila solo en windows
- **Rutas.** Todo via `filepath.Join` + `os.UserHomeDir()` — Go resuelve `%USERPROFILE%` en Windows, `$HOME` en Unix, así que `~/.pgflow.conf` y `~/.ssh/config` "simplemente funcionan".
- **TUI.** [Bubble Tea v1.3+](https://github.com/charmbracelet/bubbletea) trae soporte Windows built-in (`tty_windows.go` en la lib, abre `CONIN$` y activa VT input/output).
- **No hay flash de consola en Windows.** Al spawnear `ssh.exe` como hijo, le aplicamos `CREATE_NO_WINDOW` (0x08000000) para que no aparezca una ventana parpadeante.
- **Cero dependencias de shell.** No usamos `pkill`, `bash`, ni nada que no exista igual en los 3 OSes. Todo va por `os/exec`.

### Verificación continua (CI)

[`.github/workflows/ci.yml`](.github/workflows/ci.yml) corre en cada push/PR:

- **Job `test`:** matrix `ubuntu-latest` × `macos-latest` × `windows-latest` → `go build` + `go vet` + `gofmt -l` + `go test -race`.
- **Job `cross`:** desde `ubuntu-latest`, cross-compila los 6 targets (`darwin`/`linux`/`windows` × `amd64`/`arm64`) con `CGO_ENABLED=0`.

Si quieres probarlo en tu máquina antes de instalar:

```sh
git clone https://github.com/ander0code/pgflow.git ~/pgflow
cd ~/pgflow
go run .                  # necesita un TTY real (no pipelines)
```

### Cross-compile

```sh
make build-all            # → dist/pgflow-{darwin,linux,windows}-{amd64,arm64}[.exe]
```

Los binarios Windows llevan sufijo `.exe`; los Unix no.

## Requisitos

- **Go 1.25+** (solo para compilar).
- En el `PATH`:
  - `psql` · `pg_dump` · `pg_restore` (cliente PostgreSQL).
  - `ssh` (cliente OpenSSH).

| SO | Comando |
|---|---|
| macOS | `brew install go postgresql` |
| Debian / Ubuntu | `sudo apt install golang postgresql-client openssh-client` |
| Fedora / RHEL | `sudo dnf install golang postgresql openssh-clients` |
| Arch / Manjaro | `sudo pacman -S go postgresql-clients openssh` |
| Windows 10/11 | Instalar [PostgreSQL](https://www.postgresql.org/download/windows/). OpenSSH ya viene integrado (si no: `Add-WindowsCapability -Online -Name OpenSSH.Client~~~~0.0.1.0`). |

## Instalación

### macOS / Linux

```bash
git clone https://github.com/ander0code/pgflow.git ~/pgflow
cd ~/pgflow
bash install.sh          # compila y copia el binario a ~/.local/bin
```

Si `~/.local/bin` no está en tu `PATH`, añade a `~/.zshrc` (o `~/.bashrc`):

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Luego: `pgflow`.

Sin instalar: `go run .` · con make: `make run`.

### Windows

Desde **PowerShell** (en el directorio del repo):

```powershell
git clone https://github.com/ander0code/pgflow.git C:\tools\pgflow
cd C:\tools\pgflow
.\install.ps1           # compila y copia a %LOCALAPPDATA%\pgflow\pgflow.exe
```

Si `%LOCALAPPDATA%\pgflow` no está en tu `PATH`, agrégalo al PATH del usuario:

```powershell
[Environment]::SetEnvironmentVariable('Path',"$env:LOCALAPPDATA\pgflow;" + [Environment]::GetEnvironmentVariable('Path','User'),'User')
```

Luego abre **una nueva ventana de PowerShell** y: `pgflow`.

Para desinstalar: `.\uninstall.ps1`.

## Configuración

La configuración vive en `~/.pgflow.conf` (mismo formato que la versión
anterior, así que tu config existente sigue funcionando). Edítala a mano —
copia [`pgflow.conf.example`](pgflow.conf.example) — o desde la app en
**Configurar conexiones** (`c`).

El alias de producción debe traer su propio `LocalForward` en `~/.ssh/config`:

```
Host mi-servidor-db
    HostName 1.2.3.4
    User deploy
    LocalForward 5433 localhost:5432
```

> Las contraseñas pueden ir en `~/.pgflow.conf` (con `chmod 600` en Unix) o,
> mejor, en `~/.pgpass`. `~/.pgflow.conf` está en `.gitignore`; nunca se sube
> al repo.

### Permisos del rol de producción

`pg_dump` se ejecuta **con el rol que configures** y necesita poder leer **todas**
las tablas de la base (hace `LOCK TABLE … IN ACCESS SHARE MODE` sobre todas a la
vez). Un rol de aplicación normalmente solo accede a parte de la base, así que el
backup falla con `permission denied for table …`.

Dale lectura completa al rol, en el servidor y como admin:

```sql
-- PostgreSQL 14+
GRANT pg_read_all_data TO tu_rol;
```

En versiones anteriores: `GRANT SELECT ON ALL TABLES IN SCHEMA public TO tu_rol;`
(más `ALTER DEFAULT PRIVILEGES … GRANT SELECT …` para tablas futuras), o usa un
rol superusuario para el backup.

## Uso

```sh
pgflow                 # abre la TUI
pgflow --list          # lista los backups (texto)
pgflow --list --json   # lista los backups (JSON, para jq/pipe)
pgflow --version
pgflow --help
```

### Atajos dentro de la TUI

| Tecla | Acción |
|---|---|
| `↑` `↓` / `j` `k` | navegar |
| `g` / `G` | primero / último |
| `enter` / `space` | colapsar carpeta · restaurar el dump seleccionado |
| `b` | backup de producción (asistente) |
| `r` | restaurar a local (asistente) |
| `v` | verificar la integridad del dump |
| `d` | borrar un dump (con confirmación) |
| `t` | abrir / cerrar el túnel SSH |
| `c` | configurar / probar conexiones |
| `^r` | refrescar |
| `←` / `h` | (en los asistentes) volver un paso |
| `esc` | cancelar el asistente / cerrar modal |
| `q` / `ctrl+c` | salir |

## Cómo está construido

### Diagrama de capas

```
                        ┌─────────────────────────────────┐
                        │            main.go              │
                        │  flags CLI + arranque Tea.New   │
                        └────────────────┬────────────────┘
                                         │
                        ┌────────────────▼────────────────┐
                        │           internal/tui          │
                        │  Model · Update · View          │
                        │  pantallas · modales · commands │
                        └─┬──────────┬──────────┬────────┬┘
                          │          │          │        │
                  ┌───────▼───┐ ┌────▼────┐ ┌───▼────┐ ┌▼─────────────┐
                  │  config   │ │   pg    │ │ tunnel │ │  backups     │
                  │ ~/.pgflow │ │ psql    │ │  SSH   │ │  escaneo     │
                  │  .conf    │ │ pg_dump │ │Local-  │ │  del dir     │
                  │           │ │pg_restore│ │Forward │ │  .dump       │
                  └─────┬─────┘ └────┬────┘ └───┬────┘ └──────────────┘
                        │            │          │
                        └────────────┴──────────┘
                              config (credenciales)
```

**Reglas de dependencia (sin ciclos):**

- `tui` → {`config`, `pg`, `tunnel`, `backups`}
- `backups` → `pg`
- `pg` → `config`
- `tunnel` → `config`

### Archivos y responsabilidad

| Archivo | OS | Responsabilidad |
|---|---|---|
| `main.go` | todos | Flags CLI (`--list` / `--json` / `--version` / `--help`) + `tea.NewProgram(..., WithAltScreen())` |
| `internal/config/config.go` | todos | Lee / escribe `~/.pgflow.conf` (formato shell). Tipos `Config` y `Conn`. `Load()` / `Save()`. `expandHome()` resuelve `$HOME`, `${HOME}`, `~`. |
| `internal/pg/pg.go` | todos | Wrappers de `psql` / `pg_dump` / `pg_restore`. Dump/restore **en streaming** con `--verbose` línea por línea. `Verify` con `pg_restore --list`. `CountTables`. Clasificadores de error en español (`classifyConnError`, `classifyDumpError`, `classifyRestoreError`, `deniedObject`) — el caso de permisos parsea el objeto denegado y sugiere `GRANT pg_read_all_data`. |
| `internal/tunnel/tunnel.go` | todos | API pública: `IsUp`, `Ensure`, `Close`, `IsOpen`. Maneja el ssh subprocess con `sync.Mutex`. Reap automático en caso de fallo. |
| `internal/tunnel/tunnel_unix.go` | `!windows` | `//go:build !windows` — `hideWindow` es no-op en Unix. |
| `internal/tunnel/tunnel_windows.go` | `windows` | `//go:build windows` — `hideWindow` aplica `CREATE_NO_WINDOW` para que `ssh.exe` no haga flash de consola. |
| `internal/tunnel/tunnel_test.go` | todos | Lifecycle tests con el patrón `TestHelperProcess` (re-ejecuta el binario como ssh fake). Cubre: puerto cerrado, ya arriba, ciclo completo ensure/close, reap en fallo. |
| `internal/backups/backups.go` | todos | Escanea `BackupDir` → `[]Folder` con `[]Dump` (size, mtime, validez). `Scan`, `Total`. Verifica cada dump con `pg.Verify`. |
| `internal/tui/model.go` | todos | `Model`, `Init`, `Update`, `View`. Key handling. Wizards: backup 3 pasos, restore 4 pasos (con sub-paso 31). Estado de salud (local/túnel/prod). Splash, modales, status bar. |
| `internal/tui/render.go` | todos | Toda la UI: dashboard, detail panel, wizards, modales, splash, run-screen con logs en vivo. Helpers: `humanSize`, `truncate`, `truncateLeft`, `padR`, `padLine`. |
| `internal/tui/styles.go` | todos | Paleta Catppuccin Mocha + morado Charm `#7D56F4`. Estilos lipgloss (modales sin fondo, paneles con borde, fila seleccionada morada). |
| `internal/tui/commands.go` | todos | Funciones que devuelven `tea.Cmd` (trabajo async). Patrón `chan logEvent` para dump/restore en vivo con `waitForLog`. Timers (`tickCmd`, `spinnerTickCmd`, `splashTickCmd`, `statusClearCmd`). |
| `internal/tui/messages.go` | todos | Tipos `tea.Msg`: `scanDoneMsg`, `statusDoneMsg`, `prodDBsMsg`, `localDBsMsg`, `streamStartedMsg`, `logEventMsg`, `verifyDoneMsg`, `deleteDoneMsg`, `tunnelDoneMsg`, `connTestMsg`. |
| `internal/tui/render_smoke_test.go` | todos | `TestViewAcrossStates` (recorre todas las pantallas y verifica que `View()` no entra en pánico) + `TestSnapshot` (imprime el render con ANSI quitado, útil con `-v`). |
| `Makefile` | n/a | Targets: `build`, `run`, `test`, `vet`, `tidy`, `install`, `uninstall`, `clean`, `build-all`, `release`. PLATFORMS = los 6 targets. |
| `install.sh` / `uninstall.sh` | unix | Instalador bash para macOS / Linux (Homebrew, apt). |
| `install.ps1` / `uninstall.ps1` | windows | Instalador PowerShell para Windows. Verifica Go y herramientas runtime, build, copia a `%LOCALAPPDATA%\pgflow`, hint para añadir al PATH. |
| `.github/workflows/ci.yml` | n/a | CI matrix `ubuntu` × `macos` × `windows` + cross-build de los 6 targets. |
| `pgflow.sh` | n/a | Versión original en Bash (legacy, referencia). |
| `pgflow.conf.example` | n/a | Plantilla de config (se commitea; **sin** credenciales). |
| `AGENTS.md` | n/a | Guía para que un agente (humano o IA) entienda y modifique el repo rápido. |

### Patrón destacado: streaming de logs en vivo

`streamDumpCmd` (en `internal/tui/commands.go`) crea un `chan logEvent` con buffer,
lanza una goroutine que corre `pg.DumpStream(..., onLine)` — cada línea de stderr de
`pg_dump --verbose` se manda al canal — y termina con `logEvent{done:true, ...}`.
Devuelve `streamStartedMsg{ch}`. El modelo guarda el canal y emite
`waitForLog(ch)`, que **bloquea en `<-ch`** y devuelve `logEventMsg`. En cada evento
no-final el modelo vuelve a emitir `waitForLog`; en el final corta. La pantalla de
ejecución (`renderRunScreen`) muestra la cola de líneas + spinner + tiempo.

### Patrón destacado: errores humanos

`internal/pg/pg.go` traduce stderr de psql/pg_dump/pg_restore a mensajes
**accionables** en español. Ejemplos:

| Stderr crudo | Mensaje pgflow |
|---|---|
| `connection refused` | `conexión rechazada — ¿el túnel está caído? (pulsa t)` |
| `password authentication failed` | `contraseña incorrecta` |
| `permission denied for table auth_group` | `el rol «produser» no puede leer la tabla «auth_group». GRANT pg_read_all_data TO produser; — PostgreSQL 14+` |
| `no space left on device` | `sin espacio en disco` |
| `database already exists` (en restore) | `objetos duplicados — usa 'crear base nueva'` |

El caso de permisos parsea el tipo de objeto (`table`, `schema`, `sequence`,
`view`) y el nombre, y construye una sugerencia de `GRANT`.

## Cómo extender (recetas)

- **Nuevo campo de config:** agrégalo a `Config`/`Save`/`Load` (`config.go`) y a `buildCfgFields` (`model.go`, con su `section`).
- **Nueva acción de dashboard:** añade un `case` en `handleDashboardKey` (`model.go`) y, si es async, un comando en `commands.go` + un mensaje en `messages.go` + su `case` en `Update`.
- **Nuevo paso de asistente:** ajusta la máquina `m.step` en `*Select`/`*Back` y `crumb()`.
- **Nuevo modal:** añade la constante al enum `modal`, manéjalo en `handleModalKey` y `renderModal`.

Tras cualquier cambio de UI, corre el snapshot test para verlo:

```sh
go test ./internal/tui/ -run TestSnapshot -v
```

## Limitaciones conocidas

- **Sin scroll** en listas largas: si una carpeta tiene más dumps de los que caben en el panel, los de abajo se recortan (el cursor se mueve pero la vista no lo sigue). Pendiente: viewport con scroll (`bubbles/viewport`) en `renderBackupList` y `renderRunScreen`.
- **No se adoptan túneles externos.** Si abres el túnel por tu cuenta (sin que pgflow lo gestione), pgflow lo detecta con `IsUp` pero no puede cerrarlo. Cierra el ssh manualmente.
- **Windows < 10 1809 no soportado** porque no trae `ssh.exe` built-in. Funciona si instalas OpenSSH manualmente, pero no es ruta soportada oficialmente.

## Licencia

MIT.

> La versión original en Bash sigue disponible en [`pgflow.sh`](pgflow.sh).
