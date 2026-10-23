#!/usr/bin/env bash
# Copia de la biblioteca (pieza b del ADR-035 Copias de seguridad).
#
# Hace una instantánea de biblioteca/orig, biblioteca/aac y biblioteca/covers en
# BITABIT_BACKUP_DIR/library/<fecha UTC>/ con rsync y enlaces duros contra la
# última copia buena (library/latest), la verifica si se pide, mueve latest y
# aplica la retención. Corre como el usuario bitabit, nunca como root.
#
# Uso: backup-library.sh [--verify]
#      backup-library.sh --help
#
# Códigos de salida: 0 bien · 2 configuración no válida · 3 falló rsync o la
# copia · 4 falló la verificación · 5 falló la retención (la copia nueva queda).
set -euo pipefail

readonly SNAPSHOT_RE='^[0-9]{8}T[0-9]{6}Z$'

usage() {
	cat <<'EOF'
Uso: backup-library.sh [--verify]

Copia biblioteca/{orig,aac,covers} de BITABIT_DATA_DIR a
BITABIT_BACKUP_DIR/library/<YYYYMMDDTHHMMSSZ>/ con enlaces duros contra
library/latest, actualiza latest y aplica la retención.

  --verify   comprueba el SHA-256 de cada original copiado contra
             track_files.sha256 (calidad orig) y que no falte ninguno
  --help     muestra esta ayuda

Configuración (las variables ya definidas en el entorno tienen prioridad):
  BITABIT_ENV_FILE          (/etc/bitabit/env): BITABIT_DATA_DIR, BITABIT_DATABASE_URL
  BITABIT_BACKUP_ENV_FILE   (/etc/bitabit/backup.env): BITABIT_BACKUP_DIR,
                            BITABIT_BACKUP_KEEP_DAILY (7), BITABIT_BACKUP_KEEP_WEEKLY (4)

Salida: 0 bien, 2 configuración, 3 copia, 4 verificación, 5 retención.
EOF
}

# Mientras se configura, cualquier fallo inesperado es de configuración; cada
# fase cambia este código para que un error imprevisto salga con el suyo.
phase_code=2
partial=""

log() {
	printf 'bitabit-backup: %s\n' "$*" >&2
	# En las pruebas no se ensucia el diario del sistema.
	if [[ "${BITABIT_BACKUP_TEST:-}" != 1 ]] && command -v logger >/dev/null 2>&1; then
		# Si el registro del sistema no responde, el mensaje ya salió por stderr:
		# no merece parar la copia.
		logger -t bitabit-backup -- "$*" || printf 'bitabit-backup: logger no pudo registrar el mensaje\n' >&2
	fi
}

die() {
	local code=$1
	shift
	log "ERROR: $*"
	exit "$code"
}

on_exit() {
	local rc=$?
	if [[ -n "$partial" && -e "$partial" ]]; then
		if ! rm -rf -- "$partial"; then
			printf 'bitabit-backup: no se pudo borrar la copia a medias %s\n' "${partial##*/}" >&2
		fi
	fi
	if [[ $rc -ne 0 && ($rc -lt 2 || $rc -gt 5) ]]; then
		printf 'bitabit-backup: ERROR inesperado (código %d)\n' "$rc" >&2
		rc=$phase_code
	fi
	exit "$rc"
}
trap on_exit EXIT

# read_conf ARCHIVO NOMBRE deja en NOMBRE el valor de la última línea
# «NOMBRE=valor» del archivo, salvo que NOMBRE ya esté en el entorno. No se usa
# source: el archivo de entorno también guarda BITABIT_MEDIA_KEYS, que esta copia
# no debe cargar ni ver, y source ejecutaría lo que hubiera en él.
read_conf() {
	local file=$1 name=$2 line value="" found=0
	if [[ -n "${!name+x}" ]]; then
		return 0
	fi
	if [[ ! -e "$file" ]]; then
		return 0
	fi
	if [[ ! -r "$file" ]]; then
		log "aviso: no se puede leer el archivo de configuración ${file##*/}"
		return 0
	fi
	while IFS= read -r line || [[ -n "$line" ]]; do
		line=${line%$'\r'}
		if [[ "$line" =~ ^[[:space:]]*(export[[:space:]]+)?${name}=(.*)$ ]]; then
			value=${BASH_REMATCH[2]}
			found=1
		fi
	done <"$file"
	if [[ $found -eq 0 ]]; then
		return 0
	fi
	value=${value%"${value##*[![:space:]]}"}
	if [[ ${#value} -ge 2 ]]; then
		if [[ ( "$value" == \"*\" ) || ( "$value" == \'*\' ) ]]; then
			value=${value:1:${#value}-2}
		fi
	fi
	printf -v "$name" '%s' "$value"
}

verify=0
while [[ $# -gt 0 ]]; do
	case $1 in
	--verify) verify=1 ;;
	--help | -h)
		usage
		exit 0
		;;
	*)
		usage >&2
		die 2 "argumento desconocido: $1"
		;;
	esac
	shift
done

if [[ $EUID -eq 0 ]]; then
	die 2 "no se ejecuta como root: hay que lanzarlo como el usuario bitabit"
fi

env_file=${BITABIT_ENV_FILE:-/etc/bitabit/env}
backup_env_file=${BITABIT_BACKUP_ENV_FILE:-/etc/bitabit/backup.env}
read_conf "$env_file" BITABIT_DATA_DIR
read_conf "$env_file" BITABIT_DATABASE_URL
read_conf "$backup_env_file" BITABIT_BACKUP_DIR
read_conf "$backup_env_file" BITABIT_BACKUP_KEEP_DAILY
read_conf "$backup_env_file" BITABIT_BACKUP_KEEP_WEEKLY

data_dir=${BITABIT_DATA_DIR:-}
backup_dir=${BITABIT_BACKUP_DIR:-}
keep_daily=${BITABIT_BACKUP_KEEP_DAILY:-7}
keep_weekly=${BITABIT_BACKUP_KEEP_WEEKLY:-4}

# --- Validación (código 2) ---------------------------------------------------

[[ -n "$backup_dir" ]] || die 2 "falta BITABIT_BACKUP_DIR"
[[ "$backup_dir" == /* ]] || die 2 "BITABIT_BACKUP_DIR tiene que ser una ruta absoluta"
[[ -e "$backup_dir" ]] || die 2 "BITABIT_BACKUP_DIR no existe"
[[ -d "$backup_dir" ]] || die 2 "BITABIT_BACKUP_DIR no es una carpeta"
[[ "$(stat -L -c %u -- "$backup_dir")" == "$(id -u)" ]] ||
	die 2 "BITABIT_BACKUP_DIR no es del usuario que ejecuta la copia"
[[ "$(stat -L -c %a -- "$backup_dir")" == 700 ]] || die 2 "BITABIT_BACKUP_DIR tiene que tener modo 0700"

[[ -n "$data_dir" ]] || die 2 "falta BITABIT_DATA_DIR"
[[ "$data_dir" == /* ]] || die 2 "BITABIT_DATA_DIR tiene que ser una ruta absoluta"
[[ -d "$data_dir/biblioteca" ]] || die 2 "BITABIT_DATA_DIR no tiene carpeta biblioteca/"

# Se comparan las rutas reales para que un enlace simbólico no cuele la copia
# dentro de la carpeta de datos.
real_backup=$(realpath -e -- "$backup_dir") || die 2 "no se pudo resolver BITABIT_BACKUP_DIR"
real_data=$(realpath -e -- "$data_dir") || die 2 "no se pudo resolver BITABIT_DATA_DIR"
if [[ "$real_backup" == "$real_data" || "$real_backup" == "$real_data"/* ]]; then
	die 2 "BITABIT_BACKUP_DIR no puede estar dentro de BITABIT_DATA_DIR"
fi

[[ "$keep_daily" =~ ^[1-9][0-9]{0,3}$ ]] || die 2 "BITABIT_BACKUP_KEEP_DAILY tiene que ser un entero mayor o igual que 1"
[[ "$keep_weekly" =~ ^[1-9][0-9]{0,3}$ ]] || die 2 "BITABIT_BACKUP_KEEP_WEEKLY tiene que ser un entero mayor o igual que 1"

if [[ $verify -eq 1 ]]; then
	[[ -n "${BITABIT_DATABASE_URL:-}" ]] || die 2 "falta BITABIT_DATABASE_URL (hace falta para --verify)"
	command -v psql >/dev/null 2>&1 || die 2 "falta psql (hace falta para --verify)"
fi
command -v rsync >/dev/null 2>&1 || die 2 "falta rsync"

if [[ "${BITABIT_BACKUP_TEST:-}" == 1 && -n "${BITABIT_BACKUP_NOW:-}" ]]; then
	[[ "$BITABIT_BACKUP_NOW" =~ $SNAPSHOT_RE ]] || die 2 "BITABIT_BACKUP_NOW no tiene la forma YYYYMMDDTHHMMSSZ"
	now=$BITABIT_BACKUP_NOW
else
	now=$(date -u +%Y%m%dT%H%M%SZ)
fi

# --- Copia (código 3) --------------------------------------------------------

umask 077
library="$real_backup/library"
library_src="$real_data/biblioteca"
phase_code=3

if [[ -e "$library" && ! -d "$library" ]]; then
	die 2 "library/ existe y no es una carpeta"
fi
mkdir -p -m 0700 -- "$library"
chmod 0700 -- "$library"

link_dest=()
previous=""
if [[ -L "$library/latest" ]]; then
	previous=$(readlink -- "$library/latest")
	if [[ "$previous" =~ $SNAPSHOT_RE && -d "$library/$previous" ]]; then
		link_dest=(--link-dest="$library/$previous")
	else
		log "aviso: library/latest no apunta a una copia válida; se hace una copia completa"
		previous=""
	fi
elif [[ -e "$library/latest" ]]; then
	die 2 "library/latest existe y no es un enlace simbólico"
fi

for stale in "$library"/.*.partial; do
	if [[ -d "$stale" ]]; then
		log "aviso: queda una copia a medias de otra ejecución: library/${stale##*/}"
	fi
done

final="$library/$now"
if [[ -e "$final" || -L "$final" ]]; then
	die 3 "ya existe una copia con la fecha $now"
fi
partial="$library/.$now.partial"
mkdir -m 0700 -- "$partial"

# Las filas se leen antes de copiar: el importador coloca los archivos antes de
# confirmar su fila, así que toda fila leída aquí tiene ya su archivo en la
# biblioteca y rsync lo encontrará. Leerlas después haría fallar la verificación
# si una importación termina mientras rsync copia.
declare -A expected=()
if [[ $verify -eq 1 ]]; then
	rows=$(psql --dbname="$BITABIT_DATABASE_URL" -AtX -v ON_ERROR_STOP=1 \
		-c "SELECT track_id, ext, encode(sha256,'hex') FROM track_files WHERE quality = 'orig'" 2>/dev/null) ||
		die 4 "no se pudo leer track_files de la base de datos"
	while IFS='|' read -r id ext hex; do
		[[ -n "$id" ]] || continue
		if [[ ! "$id" =~ ^[0-9]+$ || ! "$ext" =~ ^[a-z0-9]{1,8}$ || ! "$hex" =~ ^[0-9a-f]{64}$ ]]; then
			die 4 "la base devolvió una fila de track_files inesperada"
		fi
		expected["$id.$ext"]=$hex
	done <<<"$rows"
fi

sources=()
for sub in orig aac covers; do
	if [[ -d "$library_src/$sub" && ! -L "$library_src/$sub" ]]; then
		sources+=("$library_src/$sub")
	else
		# La estructura de la copia es siempre la misma, aunque la carpeta aún no
		# exista (p. ej. sin carátulas todavía).
		log "aviso: no existe biblioteca/$sub; se deja vacía en la copia"
		mkdir -m 0700 -- "$partial/$sub"
	fi
done

# Solo se pasan orig/, aac/ y covers/: entrada/, fallidos/, las carpetas
# biblioteca/.tmp-* del importador y cualquier otra cosa de biblioteca/ quedan
# fuera. Aun así se excluye .tmp-* por si apareciera dentro de esas carpetas.
#
# --no-links: la biblioteca solo tiene archivos normales, así que un enlace
# simbólico es una anomalía. No se sigue (podría meter en la copia algo de
# fuera de la biblioteca) ni se copia como enlace (al restaurarlo apuntaría a
# donde quiera). --safe-links copiaría los que apuntan dentro del árbol, que
# tampoco deberían existir. Si un original fuera un enlace, --verify lo dará
# por perdido.
#
# --chmod fija 0700/0600 en la copia, sea cual sea el modo en la biblioteca;
# como se aplica igual en cada ejecución, --link-dest sigue enlazando los
# archivos que no cambian.
#
# Concurrencia con el importador: la base se copia antes (pieza a), así que esta
# copia puede tener una pista nueva que aún no esté en la copia de la base. Es
# aceptable: sobra un archivo sin fila, no falta ninguno de los que la base
# referencia. El importador coloca los archivos antes de confirmar su fila, y
# el rename es atómico: nunca se copia un archivo a medio escribir.
#
# Con --verify, rsync compara por contenido (--checksum) y no por tamaño y
# fecha: un original que se corrompe en el disco sin cambiar de fecha llega así
# a la copia nueva y la verificación lo detecta, en vez de enlazar en silencio
# la versión buena de ayer y no avisar nunca. Cuesta leer la biblioteca entera,
# que --verify ya hace con orig/.
checksum=()
if [[ $verify -eq 1 ]]; then
	checksum=(--checksum)
fi
if [[ ${#sources[@]} -gt 0 ]]; then
	if ! rsync -a --delete --no-links --no-devices --no-specials \
		--chmod=D0700,F0600 --exclude='.tmp-*' "${checksum[@]}" \
		"${link_dest[@]}" -- "${sources[@]}" "$partial/"; then
		die 3 "falló rsync; las copias anteriores no se tocan"
	fi
fi

# --- Verificación (código 4) -------------------------------------------------

if [[ $verify -eq 1 ]]; then
	phase_code=4
	declare -A seen=()
	bad=0
	while IFS= read -r line; do
		[[ -n "$line" ]] || continue
		hex=${line%% *}
		name=${line#*  }
		name=${name#./}
		if [[ -z "${expected[$name]+x}" ]]; then
			log "aviso: orig/$name no tiene fila en track_files (¿importado durante la copia?)"
			continue
		fi
		seen["$name"]=1
		if [[ "$hex" != "${expected[$name]}" ]]; then
			log "el SHA-256 de orig/$name no coincide con la base de datos"
			bad=$((bad + 1))
		fi
	done < <(cd -- "$partial/orig" && find . -maxdepth 1 -type f -print0 | xargs -0 -r sha256sum --)

	for name in "${!expected[@]}"; do
		if [[ -z "${seen[$name]+x}" ]]; then
			log "falta orig/$name en la copia"
			bad=$((bad + 1))
		fi
	done
	if [[ $bad -gt 0 ]]; then
		die 4 "la verificación falló en $bad archivo(s); la copia no se guarda"
	fi
	log "verificados ${#expected[@]} originales"
fi

# --- Guardar la copia y mover latest -----------------------------------------

phase_code=3
mv -T -- "$partial" "$final"
partial=""
ln -sfn -- "$now" "$library/.latest.tmp"
mv -T -- "$library/.latest.tmp" "$library/latest"
log "copia de la biblioteca guardada en library/$now"

# --- Retención (código 5) ----------------------------------------------------

phase_code=5
current=$(readlink -- "$library/latest")

snapshots=()
for dir in "$library"/*; do
	base=${dir##*/}
	if [[ -d "$dir" && ! -L "$dir" && "$base" =~ $SNAPSHOT_RE ]]; then
		snapshots+=("$base")
	fi
done
mapfile -t snapshots < <(printf '%s\n' "${snapshots[@]}" | sort -r)

declare -A keep=([$current]=1)
declare -A weeks=()
daily=0
for snap in "${snapshots[@]}"; do
	[[ -n "$snap" ]] || continue
	stamp="${snap:0:4}-${snap:4:2}-${snap:6:2} ${snap:9:2}:${snap:11:2}:${snap:13:2}"
	if ! week=$(date -u -d "$stamp" +%G-%V 2>/dev/null); then
		# Cumple el patrón pero no es una fecha: no se sabe qué es, así que ni
		# cuenta ni se borra.
		log "aviso: library/$snap no es una fecha válida; no se toca"
		keep["$snap"]=1
		continue
	fi
	if [[ $daily -lt $keep_daily ]]; then
		keep["$snap"]=1
		daily=$((daily + 1))
	fi
	# El orden es descendente: la primera de cada semana es la más reciente.
	if [[ -z "${weeks[$week]+x}" && ${#weeks[@]} -lt $keep_weekly ]]; then
		weeks["$week"]=1
		keep["$snap"]=1
	fi
done

failed=0
for snap in "${snapshots[@]}"; do
	if [[ -n "${keep[$snap]+x}" ]]; then
		continue
	fi
	dir="$library/$snap"
	if [[ -n "$snap" && "$dir" == "$library/"* && "${dir##*/}" =~ $SNAPSHOT_RE && -d "$dir" && ! -L "$dir" ]]; then
		if rm -rf -- "$dir"; then
			log "retención: borrada library/$snap"
		else
			log "retención: no se pudo borrar library/$snap"
			failed=1
		fi
	fi
done
if [[ $failed -ne 0 ]]; then
	die 5 "falló la retención; la copia nueva sí está guardada"
fi
exit 0
