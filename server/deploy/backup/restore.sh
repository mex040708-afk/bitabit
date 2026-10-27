#!/usr/bin/env bash
# Restaura una copia de seguridad de Bitabit (ADR-035) en una base de datos y una
# carpeta de datos que se le pasan. Por defecto se niega a tocar la base «bitabit»
# y /srv/bitabit: para restaurar encima del servidor de verdad hace falta la bandera
# --sobre-produccion (y parar antes el servicio).
#
# Uso:
#   restore.sh --copias <BITABIT_BACKUP_DIR> [--fecha YYYYMMDDTHHMMSSZ]
#              --base <base de datos destino> --datos <carpeta de datos destino>
#              [--sobre-produccion]
#
# Sin --fecha usa la última copia buena: el .dump más reciente de db/ y la
# instantánea a la que apunta library/latest. La base destino tiene que no existir
# (se crea) o estar vacía; la carpeta destino tiene que no existir o no tener
# biblioteca/. Tras restaurar hay que crear una clave de enlaces nueva en
# /etc/bitabit/env (la copia no la lleva: los enlaces caducan a las 12 h).
#
# Códigos de salida: 0 bien · 2 argumentos no válidos o destino no permitido ·
# 3 falló pg_restore o rsync · 4 la copia no está o está incompleta.
set -euo pipefail

prod_db=bitabit
prod_dir=/srv/bitabit

die() {
	local code=$1
	shift
	echo "restore: $*" >&2
	exit "$code"
}

backups="" when="" db="" dest="" force=0
while [[ $# -gt 0 ]]; do
	case "$1" in
	--copias) backups="${2:-}"; shift 2 ;;
	--fecha) when="${2:-}"; shift 2 ;;
	--base) db="${2:-}"; shift 2 ;;
	--datos) dest="${2:-}"; shift 2 ;;
	--sobre-produccion) force=1; shift ;;
	-h | --help)
		sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	*) die 2 "argumento desconocido: $1 (mira --help)" ;;
	esac
done

[[ -n "$backups" && -n "$db" && -n "$dest" ]] || die 2 "faltan --copias, --base o --datos (mira --help)"
[[ "$backups" == /* && "$dest" == /* ]] || die 2 "--copias y --datos tienen que ser rutas absolutas"
[[ "$db" =~ ^[a-z_][a-z0-9_]{0,62}$ ]] || die 2 "nombre de base de datos no válido: $db"
dest="$(realpath -m -- "$dest")"
if [[ $force -eq 0 && ("$db" == "$prod_db" || "$dest" == "$prod_dir" || "$dest" == "$prod_dir"/*) ]]; then
	die 2 "no se restaura sobre la base «$prod_db» ni sobre $prod_dir sin --sobre-produccion"
fi
if [[ -n "$when" && ! "$when" =~ ^[0-9]{8}T[0-9]{6}Z$ ]]; then
	die 2 "--fecha tiene que ser YYYYMMDDTHHMMSSZ"
fi

# Qué copia: la de --fecha o la última buena.
if [[ -n "$when" ]]; then
	dump="$backups/db/bitabit-$when.dump"
	snapshot="$backups/library/$when"
else
	[[ -d "$backups/db" ]] || die 4 "no hay copias de la base de datos en $backups/db"
	dump="$(find "$backups/db" -maxdepth 1 -type f -regextype posix-extended -regex '.*/bitabit-[0-9]{8}T[0-9]{6}Z\.dump' -printf '%f\n' 2>/dev/null | sort | tail -n 1)"
	[[ -n "$dump" ]] || die 4 "no hay ninguna copia de la base de datos en $backups/db"
	dump="$backups/db/$dump"
	[[ -L "$backups/library/latest" ]] || die 4 "no hay library/latest en $backups"
	snapshot="$backups/library/$(readlink -- "$backups/library/latest")"
fi
[[ -f "$dump" ]] || die 4 "no está la copia de la base de datos $dump"
[[ -d "$snapshot/orig" ]] || die 4 "la instantánea $snapshot no tiene orig/"
pg_restore --list -- "$dump" >/dev/null || die 4 "la copia $dump no se puede leer"

echo "Se va a restaurar:"
echo "  base de datos $(basename -- "$dump") -> base «$db»"
echo "  biblioteca $(basename -- "$snapshot") -> $dest/biblioteca"

# Base destino: se crea, o tiene que estar vacía.
if psql -d postgres -AtX -c "SELECT 1 FROM pg_database WHERE datname = '$db'" | grep -q 1; then
	tables="$(psql -d "$db" -AtX -c "SELECT count(*) FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog', 'information_schema')")"
	[[ "$tables" == "0" || $force -eq 1 ]] || die 2 "la base «$db» ya tiene tablas; usa otra o vacíala"
else
	createdb -- "$db" || die 3 "no se pudo crear la base «$db»"
fi
# Las funciones de búsqueda dependen de unaccent y pg_trgm (migración 0004). Una
# copia de la base entera ya trae su CREATE EXTENSION, pero una de un esquema no; son
# extensiones de confianza, así que el dueño de la base puede crearlas.
psql -d "$db" -qX -v ON_ERROR_STOP=1 \
	-c "CREATE EXTENSION IF NOT EXISTS unaccent SCHEMA public" \
	-c "CREATE EXTENSION IF NOT EXISTS pg_trgm SCHEMA public" >/dev/null ||
	die 3 "no se pudieron crear las extensiones unaccent y pg_trgm en «$db»"
pg_restore --no-owner --no-privileges --exit-on-error --dbname="$db" -- "$dump" || die 3 "falló pg_restore"

# Carpeta destino: entrada/ y fallidos/ vacías, y la biblioteca de la copia.
if [[ -e "$dest/biblioteca" && $force -eq 0 ]]; then
	die 2 "$dest/biblioteca ya existe"
fi
install -d -m 0750 -- "$dest" "$dest/entrada" "$dest/fallidos" "$dest/biblioteca"
for part in orig aac covers; do
	if [[ -d "$snapshot/$part" ]]; then
		rsync -a --delete -- "$snapshot/$part/" "$dest/biblioteca/$part/" || die 3 "falló rsync de $part/"
	fi
done
echo "Restaurado. Falta crear una clave de enlaces nueva (BITABIT_MEDIA_KEYS) antes de arrancar."
