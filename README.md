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
 │    ✗  shop_20260608_1201.dump      0B       ││ tamaño   1.7 MB             │
 │ ▸ blog  (3)                                 ││ objetos  1004               │
 │ ▾ api   (1)                                 ││ creado   2026-06-10 16:46   │
 │    ✓  api_20260601_0930.dump   4.2MB        ││ estado   ✓ válido           │
 └─────────────────────────────────────────────┘└─────────────────────────────┘
  b backup  r restore  v verify  d delete  t túnel  c config  ? ayuda  q quit
```

## Por qué pgflow

- **Un solo binario, cero runtime** — Go puro con [Bubble Tea](https://github.com/charmbracelet/bubbletea). No hay que instalar Python ni Node.
- **Flujos guiados** — backup (3 pasos) y restore (4 pasos) como asistentes con barra de progreso, resumen y confirmación. Puedes retroceder con `←`.
- **Túnel SSH automático** — abre el túnel a producción solo cuando hace falta (vía un alias de `~/.ssh/config` con `LocalForward`).
- **Integridad a la vista** — cada dump se verifica con `pg_restore --list`; los corruptos salen marcados con `✗`.
- **Restore seguro** — `--single-transaction` (todo o nada). El modo *reemplazar* (DROP + CREATE) avisa en rojo porque es irreversible.
- **Errores en cristiano** — túnel caído, credenciales, disco lleno, encoding incompatible, objetos duplicados… traducidos a mensajes accionables.
- **Modo CLI** — `pgflow --list [--json]` para scripting.

## Requisitos

- **Go 1.25+** (solo para compilar, una vez).
- `psql`, `pg_dump`, `pg_restore`, `ssh` en el `PATH`.
  - macOS: `brew install go postgresql`
  - Debian/Ubuntu: `sudo apt install golang postgresql-client openssh-client`

## Instalación

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

> Las contraseñas pueden ir en `~/.pgflow.conf` (con `chmod 600`) o, mejor, en
> `~/.pgpass`. `~/.pgflow.conf` está en `.gitignore`; nunca se sube al repo.

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

```
main.go                 flags + arranque del programa Bubble Tea
internal/config         lee / guarda ~/.pgflow.conf
internal/pg             psql · pg_dump · pg_restore · verify · errores
internal/tunnel         túnel SSH (dial TCP + ssh -f -N)
internal/backups        escaneo del directorio de backups
internal/tui            modelo Bubble Tea, estilos (lipgloss), comandos async
```

El tema visual (paleta Catppuccin Mocha + acento morado de Charm) es el mismo
de **lazyports**, para que las dos herramientas se sientan de la misma familia.

```sh
make build      # binario para tu plataforma
make test       # tests
make build-all  # cross-compile darwin/linux × amd64/arm64 → dist/
```

> La versión original en Bash sigue disponible en [`pgflow.sh`](pgflow.sh).

## Licencia

MIT.
