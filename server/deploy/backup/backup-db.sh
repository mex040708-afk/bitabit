#!/usr/bin/env bash
# Copia de la base de datos de bitabit (pieza (a) del ADR-035 Copias de
# seguridad): pg_dump -Fc a BITABIT_BACKUP_DIR/db/, verificación con
# pg_restore --list y retención de 7 diarias y 4 semanales.
#
# Uso: backup-db.sh [--help]
#
# Códigos de salida: 0 bien · 2 configuración no válida · 3 falló pg_dump ·
# 4 falló la verificación · 5 falló la retención (la copia nueva queda).
set -euo pipefail

readonly TAG="bitabit-backup"
readonly NAME_RE='^bitabit-[0-9]{8}T[0-9]{6}Z\.dump$'
readonly STAMP_RE='^[0-9]{8}T[0-9]{6}Z$'

usage() {
	cat <<'EOF'
Uso: backup-db.sh [--help]

Copia la base de datos de bitabit con pg_dump -Fc en
$BITABIT_BACKUP_DIR/db/bitabit-YYYYMMDDTHHMMSSZ.dump (fecha UTC), la
comprueba con pg_restore --list y aplica la retención: se conservan las
BITABIT_BACKUP_KEEP_DAILY copias más recientes y la más reciente de cada
una de las BITABIT_BACKUP_KEEP_WEEKLY semanas ISO más recientes.

Configuración (las variables ya definidas en el entorno tienen prioridad):
  BITABIT_ENV_FILE         (por defecto /etc/bitabit/env):
                           BITABIT_DATABASE_URL, BITABIT_DATA_DIR
  BITABIT_BACKUP_ENV_FILE  (por defecto /etc/bitabit/backup.env):
                           BITABIT_BACKUP_DIR (obligatoria),
                           BITABIT_BACKUP_KEEP_DAILY (7),
                           BITABIT_BACKUP_KEEP_WEEKLY (4)

Los archivos no se ejecutan: solo se leen las líneas NOMBRE=valor de esas
variables.

Códigos de salida: 0 bien, 2 configuración no válida, 3 falló pg_dump,
4 falló la verificación, 5 falló la retención.
EOF
}

# Mensajes por stderr y, si existe, al registro del sistema. Nunca llevan
# la URL de la base ni secretos.
log() {
	local prio="$1"
	shift
	printf '%s: %s\n' "$TAG" "$*" >&2
	if command -v logger >/dev/null 2>&1; then
		logger -t "$TAG" -p "user.$prio" -- "$*" ||
			printf '%s: aviso: logger falló\n' "$TAG" >&2
	fi
}

die() {
	local code="$1"
	shift
	log err "$*"
	exit "$code"
}

# Lee NOMBRE=valor de un archivo de configuración sin ejecutarlo (con
# source se cargaría también BITABIT_MEDIA_KEYS). Solo mira la línea de la
# variable pedida; si se repite, gana la última, como haría el shell.
read_conf_var() {
	local file="$1" name="$2" line value found=0
	if [[ -n "${!name+x}" ]]; then
		return 0
	fi
	if [[ ! -e "$file" ]]; then
		return 0
	fi
	if [[ ! -r "$file" ]]; then
		die 2 "no se puede leer el archivo de configuración $(basename -- "$file")"
	fi
	while IFS= read -r line || [[ -n "$line" ]]; do
		line="${line%$'\r'}"
		if [[ "$line" =~ ^[[:space:]]*(export[[:space:]]+)?${name}=(.*)$ ]]; then
			value="${BASH_REMATCH[2]}"
			found=1
		fi
	done <"$file"
	if ((found == 0)); then
		return 0
	fi
	if [[ ${#value} -ge 2 ]]; then
		if [[ "$value" == \"*\" || "$value" == \'*\' ]]; then
			value="${value:1:${#value}-2}"
		fi
	fi
	printf -v "$name" '%s' "$value"
}

case "${1:-}" in
-h | --help)
	usage
	exit 0
	;;
"") ;;
*)
	usage >&2
	exit 2
	;;
esac

env_file="${BITABIT_ENV_FILE:-/etc/bitabit/env}"
backup_env_file="${BITABIT_BACKUP_ENV_FILE:-/etc/bitabit/backup.env}"

read_conf_var "$env_file" BITABIT_DATABASE_URL
read_conf_var "$env_file" BITABIT_DATA_DIR
read_conf_var "$backup_env_file" BITABIT_BACKUP_DIR
read_conf_var "$backup_env_file" BITABIT_BACKUP_KEEP_DAILY
read_conf_var "$backup_env_file" BITABIT_BACKUP_KEEP_WEEKLY

backup_dir="${BITABIT_BACKUP_DIR:-}"
data_dir="${BITABIT_DATA_DIR:-}"
keep_daily="${BITABIT_BACKUP_KEEP_DAILY:-7}"
keep_weekly="${BITABIT_BACKUP_KEEP_WEEKLY:-4}"

# --- Validación de la configuración (código 2) ---

[[ -n "${BITABIT_DATABASE_URL:-}" ]] || die 2 "falta BITABIT_DATABASE_URL"
[[ -n "$backup_dir" ]] || die 2 "falta BITABIT_BACKUP_DIR"
[[ "$backup_dir" == /* ]] || die 2 "BITABIT_BACKUP_DIR debe ser una ruta absoluta"
[[ -e "$backup_dir" ]] || die 2 "BITABIT_BACKUP_DIR no existe"
[[ -d "$backup_dir" ]] || die 2 "BITABIT_BACKUP_DIR no es un directorio"
[[ "$(stat -c %u -- "$backup_dir")" == "$(id -u)" ]] ||
	die 2 "BITABIT_BACKUP_DIR no es del usuario $(id -un)"
[[ "$(stat -c %a -- "$backup_dir")" == "700" ]] ||
	die 2 "BITABIT_BACKUP_DIR debe tener modo 0700"

backup_real="$(realpath -e -- "$backup_dir")"
if [[ -n "$data_dir" ]]; then
	data_real="$(realpath -m -- "$data_dir")"
	if [[ "$backup_real" == "$data_real" || "$backup_real" == "$data_real"/* ]]; then
		die 2 "BITABIT_BACKUP_DIR no puede estar dentro de BITABIT_DATA_DIR"
	fi
fi

for pair in "BITABIT_BACKUP_KEEP_DAILY=$keep_daily" "BITABIT_BACKUP_KEEP_WEEKLY=$keep_weekly"; do
	value="${pair#*=}"
	if [[ ! "$value" =~ ^[0-9]{1,6}$ ]] || ((10#$value < 1)); then
		die 2 "${pair%%=*} debe ser un entero mayor o igual que 1"
	fi
done
keep_daily=$((10#$keep_daily))
keep_weekly=$((10#$keep_weekly))

# La fecha de «ahora» solo se puede forzar en las pruebas.
pg_dump_extra=()
if [[ "${BITABIT_BACKUP_TEST:-}" == "1" ]]; then
	if [[ -n "${BITABIT_BACKUP_NOW:-}" ]]; then
		[[ "$BITABIT_BACKUP_NOW" =~ $STAMP_RE ]] ||
			die 2 "BITABIT_BACKUP_NOW debe tener el formato YYYYMMDDTHHMMSSZ"
		stamp="$BITABIT_BACKUP_NOW"
	fi
	# Sin permiso para crear bases, la prueba copia solo un esquema temporal.
	if [[ -n "${BITABIT_BACKUP_TEST_SCHEMA:-}" ]]; then
		[[ "$BITABIT_BACKUP_TEST_SCHEMA" =~ ^[a-z_][a-z0-9_]*$ ]] ||
			die 2 "BITABIT_BACKUP_TEST_SCHEMA no es un nombre de esquema válido"
		pg_dump_extra+=("--schema=$BITABIT_BACKUP_TEST_SCHEMA")
	fi
fi
stamp="${stamp:-$(date -u +%Y%m%dT%H%M%SZ)}"

# --- Copia (código 3) y verificación (código 4) ---

umask 077
db_dir="$backup_real/db"
if [[ ! -d "$db_dir" ]]; then
	mkdir -m 0700 -- "$db_dir" || die 3 "no se pudo crear la carpeta db/"
fi
[[ "$(stat -c %a -- "$db_dir")" == "700" ]] || die 2 "la carpeta db/ debe tener modo 0700"

final_name="bitabit-$stamp.dump"
final="$db_dir/$final_name"
partial="$db_dir/.bitabit-$stamp.dump.partial"
[[ ! -e "$final" ]] || die 3 "ya existe la copia $final_name"

# Si algo falla, el .partial no se queda a medias.
cleanup() {
	if [[ -n "${partial:-}" && -e "$partial" ]]; then
		rm -f -- "$partial" || printf '%s: no se pudo borrar el .partial\n' "$TAG" >&2
	fi
}
trap cleanup EXIT

log info "copia de la base de datos: empieza $final_name"
# La URL de producción usa el socket Unix y peer: no hay contraseña en los
# argumentos. pg_dump no imprime la URL; sus errores van tal cual a stderr.
if ! pg_dump -Fc --no-owner --no-privileges "${pg_dump_extra[@]}" \
	--dbname="$BITABIT_DATABASE_URL" --file="$partial"; then
	die 3 "falló pg_dump"
fi
[[ -s "$partial" ]] || die 3 "pg_dump no escribió nada"

if ! pg_restore --list -- "$partial" >/dev/null; then
	die 4 "falló la verificación con pg_restore --list"
fi

mv -- "$partial" "$final" || die 3 "no se pudo renombrar la copia"
partial=""
chmod 0600 -- "$final" || die 3 "no se pudo poner el modo 0600 a la copia"
# sync de un archivo y su carpeta (coreutils >= 8.24) hace fsync de ambos.
sync -- "$final" "$db_dir" || log warning "sync de la copia falló"
size="$(stat -c %s -- "$final")"
log info "copia de la base de datos: hecha $final_name ($size bytes)"

# --- Retención (código 5) ---

mapfile -t names < <(
	find "$db_dir" -mindepth 1 -maxdepth 1 -type f -name 'bitabit-*.dump' -printf '%f\n' |
		LC_ALL=C sort -r
)

retention_failed=0
declare -A weeks_seen=()
weeks=0
index=0
deleted=0
for name in "${names[@]}"; do
	[[ "$name" =~ $NAME_RE ]] || continue
	index=$((index + 1))
	ts="${name#bitabit-}"
	ts="${ts%.dump}"
	if ! week="$(date -u -d "${ts:0:4}-${ts:4:2}-${ts:6:2} ${ts:9:2}:${ts:11:2}:${ts:13:2}" +%G-W%V 2>/dev/null)"; then
		log err "retención: fecha no válida en $name; no se borra"
		retention_failed=1
		continue
	fi
	keep=0
	if ((index <= keep_daily)); then
		keep=1
	fi
	# La lista va de la más reciente a la más antigua: la primera de cada
	# semana es la más reciente de esa semana.
	if [[ -z "${weeks_seen[$week]+x}" ]]; then
		weeks_seen[$week]=1
		weeks=$((weeks + 1))
		if ((weeks <= keep_weekly)); then
			keep=1
		fi
	fi
	if ((keep == 1)) || [[ "$name" == "$final_name" ]]; then
		continue
	fi
	if rm -f -- "$db_dir/$name"; then
		deleted=$((deleted + 1))
	else
		log err "retención: no se pudo borrar $name"
		retention_failed=1
	fi
done

if ((retention_failed == 1)); then
	die 5 "falló la retención de db/ (la copia nueva $final_name queda)"
fi
log info "retención de db/: $deleted borradas, $((index - deleted)) conservadas"
exit 0
