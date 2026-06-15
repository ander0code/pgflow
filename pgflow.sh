#!/usr/bin/env bash
# pgflow — backup y restore interactivo de PostgreSQL desde la terminal.
#
#   source pgflow.sh        cargar las funciones (añádelo a ~/.zshrc)
#   pgflow                  abrir el menú
#
# Las conexiones se leen de ~/.pgflow.conf (copia pgflow.conf.example).
# Requisitos: fzf, psql, pg_dump, pg_restore, ssh, nc.

# --- Defaults (se sobreescriben con ~/.pgflow.conf) ------------------
PGFLOW_CONF="${HOME}/.pgflow.conf"

PGFLOW_BACKUP_DIR="${HOME}/pgflow-backups"
PGFLOW_MIN_DISK_MB=200

PGFLOW_PROD_SSH=""             # alias de ~/.ssh/config con LocalForward
PGFLOW_PROD_HOST="localhost"
PGFLOW_PROD_PORT="5433"        # extremo local del túnel
PGFLOW_PROD_REMOTE_PORT="5432"
PGFLOW_PROD_USER="postgres"
PGFLOW_PROD_PASS=""

PGFLOW_LOCAL_HOST="localhost"
PGFLOW_LOCAL_PORT="5432"
PGFLOW_LOCAL_USER="postgres"
PGFLOW_LOCAL_PASS=""

[ -f "$PGFLOW_CONF" ] && source "$PGFLOW_CONF"
PGFLOW_LOG_DIR="${PGFLOW_BACKUP_DIR}/logs"

# --- Colores ---------------------------------------------------------
_C_RED='\033[0;31m';  _C_GREEN='\033[0;32m'; _C_YELLOW='\033[0;33m'
_C_BLUE='\033[0;34m'; _C_CYAN='\033[0;36m';  _C_BOLD='\033[1m'
_C_DIM='\033[2m';     _C_NC='\033[0m'

_banner() {
    {
        echo ""
        echo -e "  ${_C_BOLD}pgflow${_C_NC} ${_C_DIM}·${_C_NC} ${_C_BOLD}$1${_C_NC}"
        echo -e "  ${_C_DIM}────────────────────────────────────${_C_NC}"
        echo ""
    } >&2
}

# Log a archivo + consola. La consola va a stderr para no contaminar la
# salida que algunas funciones devuelven por stdout.
_log() {
    local level="$1"; shift
    local msg="$*"
    local ts; ts=$(date '+%Y-%m-%d %H:%M:%S')
    local logfile="${PGFLOW_LOG_DIR}/pgflow_$(date '+%Y%m%d').log"
    mkdir -p "$PGFLOW_LOG_DIR"
    echo "[$ts] [$level] $msg" >> "$logfile"
    {
        case "$level" in
            STEP)  echo -e "\n  ${_C_BLUE}${_C_BOLD}▸${_C_NC} ${_C_BOLD}${msg}${_C_NC}" ;;
            OK)    echo -e "  ${_C_GREEN}✓${_C_NC} ${msg}" ;;
            INFO)  echo -e "  ${_C_CYAN}·${_C_NC} ${msg}" ;;
            WARN)  echo -e "  ${_C_YELLOW}!${_C_NC} ${msg}" ;;
            ERROR) echo -e "  ${_C_RED}✗${_C_NC} ${_C_BOLD}${msg}${_C_NC}" ;;
            DIM)   echo -e "    ${_C_DIM}${msg}${_C_NC}" ;;
        esac
    } >&2
}

_ask() {
    local answer
    echo -ne "  $1 " >&2
    read -r answer
    [[ "$answer" == "s" || "$answer" == "S" ]]
}

# Fija/limpia PGPASSWORD según el destino activo.
_set_pass() {
    if [ -n "$1" ]; then export PGPASSWORD="$1"; else unset PGPASSWORD; fi
}

# --- Asistente: barra de pasos y selección con Atrás/Cancelar --------
# _fzf_step devuelve la selección por stdout y por código de salida:
#   0 = elegido   ·   2 = ESC (cancelar)   ·   3 = ← Atrás
_BACK="←  Atrás"

_crumb() {
    local cur="$1"; shift
    local total=$#
    local out="" i=1 l
    for l in "$@"; do
        if [ "$i" -eq "$cur" ]; then out="${out}[${l}]"; else out="${out}${l}"; fi
        [ "$i" -lt "$total" ] && out="${out} ─ "
        i=$((i+1))
    done
    printf 'Paso %s/%s   ·   %s' "$cur" "$total" "$out"
}

_fzf_step() {
    local prompt="$1" crumb="$2" allow_back="$3"
    local input sel hint
    input=$(cat)
    [ "$allow_back" = "1" ] && input="${_BACK}"$'\n'"${input}"
    hint="Enter elegir · ESC cancela"
    [ "$allow_back" = "1" ] && hint="${hint} · ← vuelve un paso"
    sel=$(printf "%s\n" "$input" | grep -v '^[[:space:]]*$' \
        | fzf --prompt="$prompt" --pointer='›' --height=65% --border=rounded \
              --info=inline --header="${crumb}"$'\n'"${hint}")
    [ -z "$sel" ] && return 2
    [ "$sel" = "$_BACK" ] && return 3
    printf '%s\n' "$sel"
    return 0
}

# --- Comprobaciones previas ------------------------------------------
_check_deps() {
    _log STEP "Verificando dependencias"
    local missing=()
    for cmd in fzf psql pg_dump pg_restore ssh nc; do
        command -v "$cmd" &>/dev/null || missing+=("$cmd")
    done
    if [ ${#missing[@]} -gt 0 ]; then
        _log ERROR "Faltan: ${missing[*]}"
        _log DIM  "Instala con: brew install ${missing[*]}"
        return 1
    fi
    _log OK "fzf $(fzf --version | cut -d' ' -f1), psql $(psql --version | grep -oE '[0-9]+\.[0-9]+' | head -1)"
}

_check_conn() {
    local host="$1" port="$2" user="$3" label="$4"
    _log STEP "Probando conexión a ${label} (${user}@${host}:${port})"
    local err
    err=$(psql -w -h "$host" -p "$port" -U "$user" -d postgres -c "SELECT 1;" 2>&1 1>/dev/null)
    if [ $? -ne 0 ]; then
        _log ERROR "Sin conexión a ${label}"
        if   echo "$err" | grep -qi "Connection refused";        then _log WARN "Puerto ${port} cerrado: ¿túnel caído? (pgtunnel)"
        elif echo "$err" | grep -qi "password authentication";   then _log WARN "Contraseña incorrecta para '${user}' (pgconfig)"
        elif echo "$err" | grep -qi "no password\|fe_sendauth";  then _log WARN "Falta contraseña (pgconfig o ~/.pgpass)"
        elif echo "$err" | grep -qi "not permitted to log in";   then _log WARN "El rol '${user}' no puede loguear (NOLOGIN)"
        elif echo "$err" | grep -qi "does not exist";            then _log WARN "El usuario '${user}' no existe"
        else _log DIM "$err"; fi
        return 1
    fi
    local v; v=$(psql -w -h "$host" -p "$port" -U "$user" -d postgres -t -c "SHOW server_version;" 2>/dev/null | tr -d ' ')
    _log OK "Conectado a ${label} — PostgreSQL ${v}"
}

_check_disk() {
    local dir="$1"; mkdir -p "$dir"
    local free_mb; free_mb=$(df -m "$dir" 2>/dev/null | tail -1 | awk '{print $4}')
    if [ "${free_mb:-0}" -lt "$PGFLOW_MIN_DISK_MB" ]; then
        _log WARN "Poco espacio libre: ${free_mb}MB (mínimo ${PGFLOW_MIN_DISK_MB}MB)"
        _ask "¿Continuar igual? [s/N]:" || return 1
    else
        _log OK "Espacio libre: ${free_mb}MB"
    fi
}

# --- Túnel SSH -------------------------------------------------------
_ensure_tunnel() {
    if nc -z -w2 "$PGFLOW_PROD_HOST" "$PGFLOW_PROD_PORT" 2>/dev/null; then
        _log OK "Túnel activo (${PGFLOW_PROD_HOST}:${PGFLOW_PROD_PORT})"
        return 0
    fi
    if [ -z "$PGFLOW_PROD_SSH" ]; then
        _log ERROR "Puerto ${PGFLOW_PROD_PORT} cerrado y sin alias SSH configurado"
        return 1
    fi
    _log STEP "Abriendo túnel SSH vía '${PGFLOW_PROD_SSH}'"
    ssh -f -N "$PGFLOW_PROD_SSH" 2>/tmp/pgflow_ssh.err
    local i=0
    while [ $i -lt 10 ]; do
        nc -z -w1 "$PGFLOW_PROD_HOST" "$PGFLOW_PROD_PORT" 2>/dev/null \
            && { _log OK "Túnel abierto en ${PGFLOW_PROD_HOST}:${PGFLOW_PROD_PORT}"; return 0; }
        sleep 1; i=$((i+1))
    done
    _log ERROR "No se pudo abrir el túnel vía '${PGFLOW_PROD_SSH}'"
    [ -s /tmp/pgflow_ssh.err ] && _log DIM "$(head -3 /tmp/pgflow_ssh.err)"
    return 1
}

pgtunnel() { _ensure_tunnel; }

pgtunnel-close() {
    local pids; pids=$(pgrep -f "ssh.*-N.*${PGFLOW_PROD_SSH}" 2>/dev/null)
    [ -z "$pids" ] && { _log INFO "No hay túnel abierto por pgflow"; return 0; }
    echo "$pids" | while read -r p; do kill "$p" 2>/dev/null; done
    _log OK "Túnel '${PGFLOW_PROD_SSH}' cerrado"
}

# --- Fechas (compatibles con GNU coreutils y BSD stat) ---------------
_fdate() { local o; if o=$(stat -c '%y' "$1" 2>/dev/null); then echo "${o:0:16}"; else stat -f '%Sm' -t '%Y-%m-%d %H:%M' "$1" 2>/dev/null; fi; }
_fday()  { local o; if o=$(stat -c '%y' "$1" 2>/dev/null); then echo "${o:0:10}"; else stat -f '%Sm' -t '%Y-%m-%d' "$1" 2>/dev/null; fi; }

# --- Integridad de un dump ------------------------------------------
_verify_dump() {
    local f="$1"
    _log STEP "Verificando integridad del dump"
    [ -f "$f" ] || { _log ERROR "Archivo no encontrado: $f"; return 1; }
    local size; size=$(wc -c < "$f")
    [ "${size:-0}" -eq 0 ] && { _log ERROR "Dump vacío (0 bytes): la exportación falló"; return 1; }
    local toc; toc=$(pg_restore --list "$f" 2>&1)
    if [ $? -ne 0 ]; then
        _log ERROR "Dump corrupto o formato inválido"
        _log DIM "$(echo "$toc" | head -3)"
        return 1
    fi
    local n; n=$(echo "$toc" | grep -c '^[0-9]')
    _log OK "Dump válido: $(( size / 1024 / 1024 ))MB, ${n} objetos"
}

# Recrea una DB local (DROP + CREATE) para un restore limpio.
_recreate_db() {
    local host="$1" port="$2" user="$3" db="$4" e
    psql -w -h "$host" -p "$port" -U "$user" -d postgres -c \
        "SELECT pg_terminate_backend(pid) FROM pg_stat_activity
         WHERE datname='${db}' AND pid <> pg_backend_pid();" &>/dev/null
    e=$(psql -w -h "$host" -p "$port" -U "$user" -d postgres -c "DROP DATABASE IF EXISTS \"${db}\";" 2>&1 1>/dev/null)
    if [ $? -ne 0 ]; then
        _log ERROR "No se pudo eliminar '${db}'"
        echo "$e" | grep -qi "being accessed\|other users" \
            && _log DIM "Hay conexiones activas a '${db}'; ciérralas y reintenta" \
            || _log DIM "$e"
        return 1
    fi
    e=$(psql -w -h "$host" -p "$port" -U "$user" -d postgres -c "CREATE DATABASE \"${db}\";" 2>&1 1>/dev/null)
    [ $? -ne 0 ] && { _log ERROR "No se pudo crear '${db}'"; _log DIM "$e"; return 1; }
    _log OK "Base '${db}' recreada"
}

# --- Pasos del asistente (stdout = valor; código 0/2/3) -------------
_wizard_folder() {
    local crumb="$1" base="$PGFLOW_BACKUP_DIR"
    local existing list folder count sel rc name
    mkdir -p "$base"
    while true; do
        list=""
        existing=$(find "$base" -mindepth 1 -maxdepth 1 -type d ! -name "logs" 2>/dev/null | sort | xargs -I{} basename {})
        if [ -n "$existing" ]; then
            while IFS= read -r folder; do
                count=$(find "${base}/${folder}" -name "*.dump" -type f 2>/dev/null | wc -l | tr -d ' ')
                list+=$(printf "%-26s  %s backup(s)" "$folder" "$count"); list+=$'\n'
            done <<< "$existing"
        fi
        list+="+ Crear carpeta nueva"
        sel=$(printf "%s" "$list" | _fzf_step "carpeta destino  " "$crumb" 1); rc=$?
        [ $rc -ne 0 ] && return $rc
        if echo "$sel" | grep -q "Crear carpeta nueva"; then
            echo -ne "\n  Nombre de la carpeta ${_C_DIM}(vacío + Enter = volver)${_C_NC}: " >&2
            read -r name
            [ -z "$name" ] && continue
            name=$(echo "$name" | tr ' ' '-' | tr -cd '[:alnum:]-_.')
            mkdir -p "${base}/${name}"; printf '%s\n' "${base}/${name}"; return 0
        fi
        name=$(echo "$sel" | awk '{print $1}')
        mkdir -p "${base}/${name}"; printf '%s\n' "${base}/${name}"; return 0
    done
}

_wizard_restore_folder() {
    local crumb="$1" base="$PGFLOW_BACKUP_DIR"
    local list folder count newest f sel rc name
    list=$(find "$base" -mindepth 1 -maxdepth 1 -type d ! -name "logs" 2>/dev/null | sort \
           | while IFS= read -r folder; do
               count=$(find "$folder" -name "*.dump" -type f 2>/dev/null | wc -l | tr -d ' ')
               newest=""
               [ "$count" -gt 0 ] && newest=$(_fday "$(find "$folder" -name "*.dump" -type f | sort -r | head -1)")
               printf "%-24s  %2s backup(s)   últ: %s\n" "$(basename "$folder")" "$count" "${newest:--}"
             done)
    [ -z "$list" ] && { _log ERROR "No hay carpetas de backups en ${base}"; _log DIM "Ejecuta primero un backup"; return 2; }
    sel=$(printf "%s\n" "$list" | _fzf_step "carpeta de backups  " "$crumb" 0); rc=$?
    [ $rc -ne 0 ] && return $rc
    name=$(echo "$sel" | awk '{print $1}')
    printf '%s\n' "${base}/${name}"
}

_wizard_restore_dump() {
    local folder_path="$1" crumb="$2"
    local dumps list f sz dt ok sel rc name
    dumps=$(find "$folder_path" -name "*.dump" -type f 2>/dev/null | sort -r)
    [ -z "$dumps" ] && { _log ERROR "No hay dumps en esa carpeta"; return 3; }
    list=$(printf "%s\n" "$dumps" | while IFS= read -r f; do
        [ -z "$f" ] && continue
        sz=$(du -sh "$f" 2>/dev/null | cut -f1)
        dt=$(_fdate "$f")
        if pg_restore --list "$f" &>/dev/null; then ok="✓"; else ok="✗corrupto"; fi
        printf "%s  %-46s  %6s  %s\n" "$ok" "$(basename "$f")" "$sz" "$dt"
    done)
    sel=$(printf "%s\n" "$list" | _fzf_step "backup (↑ más reciente)  " "$crumb" 1); rc=$?
    [ $rc -ne 0 ] && return $rc
    name=$(echo "$sel" | awk '{print $2}')
    printf '%s\n' "${folder_path}/${name}"
}

# Devuelve "CREATE <db>" o "REPLACE <db>".
_wizard_restore_target() {
    local crumb="$1" dumpfile="$2"
    local sel rc name suggested exists locals picked
    sel=$(printf "%s\n" "Crear una base de datos nueva" "Usar una base existente" \
          | _fzf_step "destino del restore  " "$crumb" 1); rc=$?
    [ $rc -ne 0 ] && return $rc
    if echo "$sel" | grep -qi "crear"; then
        suggested=$(basename "$dumpfile" .dump | sed 's/_[0-9]\{8\}_[0-9]\{6\}$//')_local
        echo -ne "\n  Nombre de la base nueva ${_C_DIM}[Enter = ${suggested}]${_C_NC}: " >&2
        read -r name; name="${name:-$suggested}"
        exists=$(psql -w -h "$PGFLOW_LOCAL_HOST" -p "$PGFLOW_LOCAL_PORT" -U "$PGFLOW_LOCAL_USER" -d postgres \
                 -t -c "SELECT 1 FROM pg_database WHERE datname='${name}';" 2>/dev/null | tr -d '[:space:]')
        [ "$exists" = "1" ] && printf 'REPLACE %s\n' "$name" || printf 'CREATE %s\n' "$name"
        return 0
    fi
    locals=$(psql -w -h "$PGFLOW_LOCAL_HOST" -p "$PGFLOW_LOCAL_PORT" -U "$PGFLOW_LOCAL_USER" -d postgres \
             -t -c "SELECT datname FROM pg_database
                    WHERE datistemplate=false AND datname NOT IN ('postgres','rdsadmin')
                    ORDER BY datname;" 2>/dev/null | sed 's/^[[:space:]]*//' | grep -v '^$')
    [ -z "$locals" ] && { _log WARN "No hay bases locales; elige 'crear nueva'"; return 3; }
    picked=$(printf "%s\n" "$locals" | _fzf_step "base local destino (se reemplaza)  " "$crumb" 1); rc=$?
    [ $rc -ne 0 ] && return $rc
    printf 'REPLACE %s\n' "$picked"
}

_summary_backup() {
    {
        echo ""
        echo -e "  ${_C_BOLD}Resumen${_C_NC}"
        echo -e "  ${_C_DIM}base${_C_NC}     $1"
        echo -e "  ${_C_DIM}origen${_C_NC}   ${PGFLOW_PROD_USER}@${PGFLOW_PROD_HOST}:${PGFLOW_PROD_PORT} (túnel '${PGFLOW_PROD_SSH}')"
        echo -e "  ${_C_DIM}carpeta${_C_NC}  $2"
        echo -e "  ${_C_DIM}archivo${_C_NC}  $1_$(date '+%Y%m%d_%H%M%S').dump"
        echo ""
    } >&2
}

_summary_restore() {
    local dumpfile="$1" mode="$2" target="$3" m
    [ "$mode" = "REPLACE" ] && m="${_C_RED}reemplazar (DROP + CREATE, irreversible)${_C_NC}" || m="${_C_GREEN}crear nueva${_C_NC}"
    {
        echo ""
        echo -e "  ${_C_BOLD}Resumen${_C_NC}"
        echo -e "  ${_C_DIM}backup${_C_NC}   $(basename "$dumpfile") ($(du -sh "$dumpfile" 2>/dev/null | cut -f1))"
        echo -e "  ${_C_DIM}destino${_C_NC}  ${target}  (${PGFLOW_LOCAL_USER}@${PGFLOW_LOCAL_HOST}:${PGFLOW_LOCAL_PORT})"
        echo -e "  ${_C_DIM}modo${_C_NC}     ${m}"
        echo ""
    } >&2
}

# --- Backup (asistente de 3 pasos) ----------------------------------
pgbackup() {
    _banner "Backup de producción"
    _set_pass "$PGFLOW_PROD_PASS"
    _check_deps    || return 1
    _ensure_tunnel || return 1
    _check_conn "$PGFLOW_PROD_HOST" "$PGFLOW_PROD_PORT" "$PGFLOW_PROD_USER" "Producción" || return 1
    _check_disk "$PGFLOW_BACKUP_DIR" || return 1

    _log STEP "Cargando bases de datos"
    local dbs db subdir step rc sel ts dumpfile errlog t0 code elapsed err warn l
    dbs=$(psql -w -h "$PGFLOW_PROD_HOST" -p "$PGFLOW_PROD_PORT" -U "$PGFLOW_PROD_USER" -d postgres \
              -t -c "SELECT datname FROM pg_database
                     WHERE datistemplate=false AND datname NOT IN ('postgres','rdsadmin')
                     ORDER BY datname;" 2>/dev/null | sed 's/^[[:space:]]*//' | grep -v '^$')
    [ -z "$dbs" ] && { _log ERROR "No se encontraron bases de datos"; return 1; }

    step=1
    while true; do
        case $step in
        1) db=$(printf "%s\n" "$dbs" | _fzf_step "base a respaldar  " "$(_crumb 1 Base Carpeta Confirmar)" 0); rc=$?
           [ $rc -eq 2 ] && { _log WARN "Cancelado"; return 0; }
           step=2 ;;
        2) subdir=$(_wizard_folder "$(_crumb 2 Base Carpeta Confirmar)"); rc=$?
           [ $rc -eq 2 ] && { _log WARN "Cancelado"; return 0; }
           [ $rc -eq 3 ] && { step=1; continue; }
           step=3 ;;
        3) _summary_backup "$db" "$subdir"
           sel=$(printf "%s\n" "Sí, ejecutar el backup" | _fzf_step "confirmar  " "$(_crumb 3 Base Carpeta Confirmar)" 1); rc=$?
           [ $rc -eq 2 ] && { _log WARN "Cancelado"; return 0; }
           [ $rc -eq 3 ] && { step=2; continue; }
           break ;;
        esac
    done

    ts=$(date '+%Y%m%d_%H%M%S')
    dumpfile="${subdir}/${db}_${ts}.dump"
    errlog="${PGFLOW_LOG_DIR}/pgdump_${db}_${ts}.err"
    _log STEP "Exportando (pg_dump)"
    t0=$SECONDS
    pg_dump -h "$PGFLOW_PROD_HOST" -p "$PGFLOW_PROD_PORT" -U "$PGFLOW_PROD_USER" \
            -F c --no-password -f "$dumpfile" "$db" 2>"$errlog"
    code=$?; elapsed=$(( SECONDS - t0 ))

    if [ $code -ne 0 ]; then
        _log ERROR "pg_dump falló (código ${code}, ${elapsed}s)"
        err=$(cat "$errlog" 2>/dev/null)
        if   echo "$err" | grep -qi "could not connect\|connection refused"; then _log WARN "Se perdió la conexión (¿túnel caído?)"
        elif echo "$err" | grep -qi "no password\|password authentication";  then _log WARN "Problema de autenticación (pgconfig)"
        elif echo "$err" | grep -qi "permission denied";                      then _log WARN "Sin permisos para exportar '${db}'"
        elif echo "$err" | grep -qi "No space left";                          then _log WARN "Sin espacio en disco"
        else _log DIM "Detalle:"; head -5 "$errlog" | while IFS= read -r l; do _log DIM "$l"; done; fi
        _log DIM "Log: ${errlog}"
        [ -f "$dumpfile" ] && rm -f "$dumpfile" && _log DIM "Dump incompleto eliminado"
        return 1
    fi

    _verify_dump "$dumpfile" || return 1
    _log OK "Backup listo en ${elapsed}s — $(basename "$dumpfile")"
    _log DIM "$dumpfile"
    if [ -s "$errlog" ]; then
        warn=$(grep -ci "warning" "$errlog" 2>/dev/null); warn=${warn:-0}
        [ "$warn" -gt 0 ] && _log WARN "${warn} aviso(s) no fatal(es): ${errlog}"
    fi
}

# --- Restore (asistente de 4 pasos) ---------------------------------
pgrestore-local() {
    _banner "Restore a local"
    _set_pass "$PGFLOW_LOCAL_PASS"
    _check_deps || return 1
    _check_conn "$PGFLOW_LOCAL_HOST" "$PGFLOW_LOCAL_PORT" "$PGFLOW_LOCAL_USER" "Local" || return 1

    local step rc folder_path dumpfile out mode target sel cerr
    local ts errlog t0 code elapsed err warn tables l
    local LBL=(Carpeta Backup Destino Confirmar)

    step=1
    while true; do
        case $step in
        1) folder_path=$(_wizard_restore_folder "$(_crumb 1 "${LBL[@]}")"); rc=$?
           [ $rc -eq 2 ] && { _log WARN "Cancelado"; return 0; }
           step=2 ;;
        2) dumpfile=$(_wizard_restore_dump "$folder_path" "$(_crumb 2 "${LBL[@]}")"); rc=$?
           [ $rc -eq 2 ] && { _log WARN "Cancelado"; return 0; }
           [ $rc -eq 3 ] && { step=1; continue; }
           step=3 ;;
        3) out=$(_wizard_restore_target "$(_crumb 3 "${LBL[@]}")" "$dumpfile"); rc=$?
           [ $rc -eq 2 ] && { _log WARN "Cancelado"; return 0; }
           [ $rc -eq 3 ] && { step=2; continue; }
           mode=${out%% *}; target=${out#* }
           step=4 ;;
        4) _summary_restore "$dumpfile" "$mode" "$target"
           sel=$(printf "%s\n" "Sí, restaurar ahora" | _fzf_step "confirmar  " "$(_crumb 4 "${LBL[@]}")" 1); rc=$?
           [ $rc -eq 2 ] && { _log WARN "Cancelado"; return 0; }
           [ $rc -eq 3 ] && { step=3; continue; }
           break ;;
        esac
    done

    _verify_dump "$dumpfile" || return 1

    if [ "$mode" = "REPLACE" ]; then
        _recreate_db "$PGFLOW_LOCAL_HOST" "$PGFLOW_LOCAL_PORT" "$PGFLOW_LOCAL_USER" "$target" || return 1
    else
        _log INFO "Creando base local '${target}'"
        cerr=$(psql -w -h "$PGFLOW_LOCAL_HOST" -p "$PGFLOW_LOCAL_PORT" -U "$PGFLOW_LOCAL_USER" -d postgres \
               -c "CREATE DATABASE \"${target}\";" 2>&1 1>/dev/null)
        if [ $? -ne 0 ]; then
            _log ERROR "No se pudo crear '${target}'"
            echo "$cerr" | grep -qi "permission denied" && _log DIM "El usuario necesita rol CREATEDB" || _log DIM "$cerr"
            return 1
        fi
        _log OK "Base '${target}' creada"
    fi

    _log STEP "Restaurando hacia '${target}'"
    ts=$(date '+%Y%m%d_%H%M%S')
    errlog="${PGFLOW_LOG_DIR}/pgrestore_${target}_${ts}.err"
    t0=$SECONDS
    pg_restore -h "$PGFLOW_LOCAL_HOST" -p "$PGFLOW_LOCAL_PORT" -U "$PGFLOW_LOCAL_USER" \
               -d "$target" -F c --no-owner --no-privileges --single-transaction "$dumpfile" 2>"$errlog"
    code=$?; elapsed=$(( SECONDS - t0 ))

    if [ $code -eq 0 ]; then
        _log OK "Restore correcto (${elapsed}s)"
    elif [ $code -eq 1 ]; then
        warn=$(grep -c "ERROR\|WARNING" "$errlog" 2>/dev/null); warn=${warn:-0}
        _log WARN "Restore con ${warn} aviso(s) no fatal(es) (${elapsed}s)"
        _log DIM "Suele ser por roles/extensiones inexistentes en local — ${errlog}"
    else
        _log ERROR "Restore falló (código ${code}, ${elapsed}s)"
        _log DIM "Con --single-transaction no se aplicó nada (la base quedó vacía)"
        err=$(cat "$errlog" 2>/dev/null)
        if   echo "$err" | grep -qi "could not connect\|connection refused"; then _log WARN "Se perdió la conexión local"
        elif echo "$err" | grep -qi "already exists";   then _log WARN "Objetos duplicados — usa 'crear nueva'"
        elif echo "$err" | grep -qi "encoding";         then _log WARN "Encoding incompatible (UTF8 vs LATIN1)"
        elif echo "$err" | grep -qi "invalid";          then _log WARN "Dump dañado — genera uno nuevo"
        elif echo "$err" | grep -qi "No space left";    then _log WARN "Sin espacio en disco"
        else _log DIM "Detalle:"; head -10 "$errlog" | while IFS= read -r l; do _log DIM "$l"; done; fi
        _log DIM "Log: ${errlog}"
        return 1
    fi

    _log STEP "Comprobación posterior"
    tables=$(psql -w -h "$PGFLOW_LOCAL_HOST" -p "$PGFLOW_LOCAL_PORT" -U "$PGFLOW_LOCAL_USER" -d "$target" \
             -t -c "SELECT COUNT(*) FROM information_schema.tables
                    WHERE table_schema='public' AND table_type='BASE TABLE';" 2>/dev/null | tr -d '[:space:]')
    [ "${tables:-0}" -gt 0 ] \
        && _log OK "${tables} tablas en '${target}'" \
        || _log WARN "Sin tablas en el schema public (¿otro schema?)"
    _log DIM "psql -h ${PGFLOW_LOCAL_HOST} -p ${PGFLOW_LOCAL_PORT} -U ${PGFLOW_LOCAL_USER} -d ${target}"
}

# --- Listar / verificar ---------------------------------------------
pglist() {
    local base="$PGFLOW_BACKUP_DIR"
    { echo ""; echo -e "  ${_C_BOLD}Backups${_C_NC} ${_C_DIM}· ${base}${_C_NC}"; echo ""; } >&2
    local folders folder dumps f sz dt v n total
    total=0
    folders=$(find "$base" -mindepth 1 -maxdepth 1 -type d ! -name "logs" 2>/dev/null | sort)
    [ -z "$folders" ] && { echo -e "  ${_C_DIM}(sin backups todavía)${_C_NC}" >&2; return; }
    while IFS= read -r folder; do
        [ -z "$folder" ] && continue
        dumps=$(find "$folder" -name "*.dump" -type f 2>/dev/null | sort -r)
        n=$(printf "%s" "$dumps" | grep -c .)
        echo -e "  ${_C_BOLD}${_C_CYAN}$(basename "$folder")${_C_NC} ${_C_DIM}· ${n} backup(s)${_C_NC}" >&2
        if [ -n "$dumps" ]; then
            while IFS= read -r f; do
                [ -z "$f" ] && continue
                sz=$(du -sh "$f" 2>/dev/null | cut -f1)
                dt=$(_fdate "$f")
                if pg_restore --list "$f" &>/dev/null; then v="${_C_GREEN}✓${_C_NC}"; else v="${_C_RED}✗${_C_NC}"; fi
                printf "    %b  %-46s  %6s  %s\n" "$v" "$(basename "$f")" "$sz" "$dt" >&2
                total=$((total+1))
            done <<< "$dumps"
        fi
        echo "" >&2
    done <<< "$folders"
    echo -e "  ${_C_DIM}${total} backup(s) en total${_C_NC}" >&2
}

pgverify() {
    _log STEP "Elige el dump a verificar"
    local dumps sel
    dumps=$(find "$PGFLOW_BACKUP_DIR" -name "*.dump" -type f 2>/dev/null | sort -r)
    [ -z "$dumps" ] && { _log ERROR "No hay dumps en ${PGFLOW_BACKUP_DIR}"; return 1; }
    sel=$(echo "$dumps" | xargs -I{} basename {} | fzf --prompt="verificar  " --pointer='›' --height=50% --border=rounded)
    [ -z "$sel" ] && return 0
    _verify_dump "$(find "$PGFLOW_BACKUP_DIR" -name "$sel" -type f | head -1)"
}

# --- Configuración ---------------------------------------------------
_save_conf() {
    umask 077
    cat > "$PGFLOW_CONF" <<EOF
# pgflow — conexiones (lo gestiona pgconfig)
PGFLOW_LOCAL_HOST="${PGFLOW_LOCAL_HOST}"
PGFLOW_LOCAL_PORT="${PGFLOW_LOCAL_PORT}"
PGFLOW_LOCAL_USER="${PGFLOW_LOCAL_USER}"
PGFLOW_LOCAL_PASS="${PGFLOW_LOCAL_PASS}"
PGFLOW_PROD_SSH="${PGFLOW_PROD_SSH}"
PGFLOW_PROD_HOST="${PGFLOW_PROD_HOST}"
PGFLOW_PROD_PORT="${PGFLOW_PROD_PORT}"
PGFLOW_PROD_REMOTE_PORT="${PGFLOW_PROD_REMOTE_PORT}"
PGFLOW_PROD_USER="${PGFLOW_PROD_USER}"
PGFLOW_PROD_PASS="${PGFLOW_PROD_PASS}"
PGFLOW_BACKUP_DIR="${PGFLOW_BACKUP_DIR}"
EOF
    chmod 600 "$PGFLOW_CONF"
}

_prompt_field() {
    local varname="$1" label="$2" secret="$3"
    local cur shown val
    eval "cur=\${$varname}"
    shown="$cur"
    [ "$secret" = "secret" ] && [ -n "$cur" ] && shown="••••"
    echo -ne "  ${label} ${_C_DIM}[${shown:-vacío}]${_C_NC}: " >&2
    read -r val
    [ -n "$val" ] && eval "$varname=\"\$val\""
}

_edit_conn() {
    { echo ""; echo -e "  ${_C_BOLD}Editar $1${_C_NC} ${_C_DIM}(Enter = mantener)${_C_NC}"; } >&2
    if [ "$1" = "local" ]; then
        _prompt_field PGFLOW_LOCAL_HOST "Host"
        _prompt_field PGFLOW_LOCAL_PORT "Puerto"
        _prompt_field PGFLOW_LOCAL_USER "Usuario"
        _prompt_field PGFLOW_LOCAL_PASS "Password" secret
    else
        _prompt_field PGFLOW_PROD_SSH         "Alias SSH"
        _prompt_field PGFLOW_PROD_PORT        "Puerto local del túnel"
        _prompt_field PGFLOW_PROD_REMOTE_PORT "Puerto postgres remoto"
        _prompt_field PGFLOW_PROD_USER        "Usuario"
        _prompt_field PGFLOW_PROD_PASS        "Password" secret
    fi
    _save_conf
    _log OK "Guardado en ${PGFLOW_CONF}"
}

pgconfig() {
    local opt
    while true; do
        {
            echo ""
            echo -e "  ${_C_BOLD}Conexiones${_C_NC}"
            echo -e "  ${_C_CYAN}local${_C_NC}  ${PGFLOW_LOCAL_USER}@${PGFLOW_LOCAL_HOST}:${PGFLOW_LOCAL_PORT}  pass:$([ -n "$PGFLOW_LOCAL_PASS" ] && echo '••••' || echo '—')"
            echo -e "  ${_C_CYAN}prod${_C_NC}   ${PGFLOW_PROD_USER}@${PGFLOW_PROD_HOST}:${PGFLOW_PROD_PORT}  ssh:'${PGFLOW_PROD_SSH:-—}'  pass:$([ -n "$PGFLOW_PROD_PASS" ] && echo '••••' || echo '—')"
            echo ""
        } >&2
        opt=$(printf "%s\n" \
            "Editar conexión LOCAL" \
            "Editar conexión PRODUCCIÓN" \
            "Probar conexión LOCAL" \
            "Probar conexión PRODUCCIÓN" \
            "Abrir ~/.pgflow.conf en el editor" \
            "Volver" \
            | fzf --prompt="config  " --pointer='›' --height=45% --border=rounded --no-info)
        case "$opt" in
            *"Editar conexión LOCAL"*)       _edit_conn local ;;
            *"Editar conexión PRODUCCIÓN"*)  _edit_conn prod ;;
            *"Probar conexión LOCAL"*)
                _set_pass "$PGFLOW_LOCAL_PASS"
                _check_conn "$PGFLOW_LOCAL_HOST" "$PGFLOW_LOCAL_PORT" "$PGFLOW_LOCAL_USER" "Local" ;;
            *"Probar conexión PRODUCCIÓN"*)
                _set_pass "$PGFLOW_PROD_PASS"
                _ensure_tunnel && _check_conn "$PGFLOW_PROD_HOST" "$PGFLOW_PROD_PORT" "$PGFLOW_PROD_USER" "Producción" ;;
            *"pgflow.conf"*)
                ${EDITOR:-nano} "$PGFLOW_CONF"; [ -f "$PGFLOW_CONF" ] && source "$PGFLOW_CONF" && _log OK "Config recargada" ;;
            *) break ;;
        esac
    done
}

# --- Menú principal --------------------------------------------------
pgflow() {
    local choice
    while true; do
        choice=$(printf "%s\n" \
            "Backup de producción" \
            "Restaurar a local" \
            "Ver backups" \
            "Verificar un dump" \
            "Configurar conexiones" \
            "Cerrar túnel SSH" \
            "Salir" \
          | fzf --prompt="pgflow  " --pointer='›' --height=45% --border=rounded \
                --header="¿Qué quieres hacer?  ·  ESC sale" --no-info)
        case "$choice" in
            *Backup*)        pgbackup ;;
            *Restaurar*)     pgrestore-local ;;
            *"Ver backups"*) pglist ;;
            *Verificar*)     pgverify ;;
            *Configurar*)    pgconfig ;;
            *Cerrar*)        pgtunnel-close ;;
            *)               break ;;
        esac
        echo -ne "\n  ${_C_DIM}Enter para volver al menú${_C_NC}" >&2; read -r _dummy
    done
}

pghelp() {
    {
        echo ""
        echo -e "  ${_C_BOLD}pgflow${_C_NC}"
        echo -e "  ${_C_CYAN}pgflow${_C_NC}           menú principal"
        echo -e "  ${_C_CYAN}pgbackup${_C_NC}         backup de producción"
        echo -e "  ${_C_CYAN}pgrestore-local${_C_NC}  restore a local"
        echo -e "  ${_C_CYAN}pgconfig${_C_NC}         ver / editar / probar conexiones"
        echo -e "  ${_C_CYAN}pgtunnel${_C_NC} · ${_C_CYAN}pgtunnel-close${_C_NC}   abrir / cerrar túnel"
        echo -e "  ${_C_CYAN}pglist${_C_NC} · ${_C_CYAN}pgverify${_C_NC}           listar / verificar"
        echo ""
        echo -e "  ${_C_DIM}En los asistentes: Enter elige · ESC cancela · ← vuelve un paso${_C_NC}"
        echo -e "  ${_C_DIM}config: ${PGFLOW_CONF}${_C_NC}"
        echo ""
    } >&2
}

echo -e "  ${_C_GREEN}✓${_C_NC} pgflow — escribe ${_C_CYAN}pgflow${_C_NC} o ${_C_CYAN}pghelp${_C_NC}"
