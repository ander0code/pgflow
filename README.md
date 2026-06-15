# pgflow

Backup y restore de PostgreSQL desde la terminal, con un asistente
interactivo (fzf). Pensado para el flujo "respaldo una base de producción
por túnel SSH y la restauro en mi PostgreSQL local" sin tener que recordar
banderas de `pg_dump` / `pg_restore` ni rutas de carpetas.

Es un solo script en Bash. Sin dependencias de lenguaje, sin base de datos
propia: la configuración vive en `~/.pgflow.conf` y los backups son archivos
`.dump` en disco.

## Requisitos

- `fzf`, `psql`, `pg_dump`, `pg_restore`, `ssh`, `nc`
- macOS: `brew install fzf postgresql`
- Linux (Debian/Ubuntu): `sudo apt install fzf postgresql-client netcat-openbsd`

## Instalación

```sh
git clone <este-repo> ~/pgflow
echo 'source ~/pgflow/pgflow.sh' >> ~/.zshrc   # o ~/.bashrc
cp ~/pgflow/pgflow.conf.example ~/.pgflow.conf
chmod 600 ~/.pgflow.conf
```

Abre una terminal nueva (o `source ~/.zshrc`) y escribe `pgflow`.

## Configuración

Edita `~/.pgflow.conf` a mano, o desde la app en **Configurar conexiones**.
Hay dos conexiones:

- **local**: tu PostgreSQL de desarrollo.
- **producción**: se alcanza por un túnel SSH. El alias de
  `~/.ssh/config` debe traer su propio `LocalForward`:

  ```
  Host mi-servidor-db
      HostName 1.2.3.4
      User deploy
      LocalForward 5433 localhost:5432
  ```

  Con eso, `pgflow` abre el túnel solo (`ssh -f -N mi-servidor-db`) cuando
  hace falta.

> Las contraseñas pueden ir en `~/.pgflow.conf` (con `chmod 600`) o, mejor,
> en `~/.pgpass`. El archivo `~/.pgflow.conf` está en `.gitignore`; nunca se
> sube al repositorio.

## Uso

```sh
pgflow            # menú
```

| Comando           | Qué hace                                  |
|-------------------|-------------------------------------------|
| `pgflow`          | Menú principal                            |
| `pgbackup`        | Backup de producción (3 pasos)            |
| `pgrestore-local` | Restore a local (4 pasos)                 |
| `pgconfig`        | Ver / editar / probar conexiones          |
| `pgtunnel`        | Abrir el túnel SSH                         |
| `pgtunnel-close`  | Cerrar el túnel SSH                        |
| `pglist`          | Listar backups agrupados por carpeta      |
| `pgverify`        | Verificar la integridad de un dump        |

En los asistentes: **Enter** elige, **ESC** cancela, **←** vuelve un paso.
Antes de ejecutar siempre se muestra un resumen para confirmar.

### Backup

1. Elegir la base de datos de producción.
2. Elegir o crear la carpeta destino (se organiza por proyecto).
3. Confirmar el resumen.

El archivo se guarda como `<base>_<AAAAMMDD_HHMMSS>.dump` (formato custom de
`pg_dump`, `-F c`) y se verifica al terminar.

### Restore

1. Elegir la carpeta.
2. Elegir el dump (los corruptos salen marcados con `✗`).
3. Elegir destino: crear una base nueva o reemplazar una existente.
4. Confirmar.

El restore usa `--single-transaction`: si algo falla, no se aplica nada.
Reemplazar una base hace `DROP` + `CREATE` (es irreversible; se avisa en el
resumen).

## Manejo de errores

Cada operación deja un log en `<carpeta-de-backups>/logs/`. El script intenta
explicar las fallas más comunes: túnel caído, credenciales incorrectas, disco
lleno, dump vacío o corrupto, incompatibilidad de encoding, objetos
duplicados y pérdida de conexión a mitad del proceso.

## Cómo está construido

Un único archivo, `pgflow.sh`, que se carga con `source`. Las piezas:

- **Configuración**: valores por defecto en el script, sobreescritos por
  `~/.pgflow.conf`. Sin credenciales en el código.
- **`_log`**: registra a archivo y muestra en pantalla. La salida visible va
  a *stderr*, para no contaminar los valores que las funciones devuelven por
  *stdout*.
- **Asistente** (`_fzf_step` + `_crumb`): cada paso es una llamada a `fzf`
  con una barra de progreso ("Paso 2/4") en la cabecera. La función devuelve
  un código que el bucle interpreta: elegido, cancelar (ESC) o volver (←).
  Los flujos de backup y restore son máquinas de estados sobre ese código,
  por eso se puede retroceder entre pasos.
- **Túnel**: se abre con el alias de SSH solo si el puerto no responde
  (`nc -z`).
- **Fechas**: se detecta si `stat` es de GNU coreutils o BSD, porque la
  sintaxis difiere.

### Notas para hackear el código

- Es compatible con `bash` y `zsh`. En `zsh`, redeclarar `local x` (sin
  valor) dentro de un bucle imprime `x=valor`; por eso todas las variables se
  declaran una sola vez al inicio de cada función y se asignan dentro del
  bucle.
- Las funciones que "devuelven" un valor lo hacen por *stdout* con un solo
  `printf` al final; todo lo demás (logs, prompts) va a *stderr*.

## Licencia

MIT.
