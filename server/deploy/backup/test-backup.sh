#!/usr/bin/env bash
# Prueba de punta a punta de las copias de seguridad (ADR-035): importa una pista
# de verdad con el binario de bitabit, hace la copia de la base de datos y de la
# biblioteca con los scripts de esta carpeta, las restaura en otro sitio y compara.
#
# No toca la base «bitabit» más que con un esquema temporal (el usuario bitabit no
# puede crear bases), ni /srv/bitabit ni /etc/bitabit: todo va a /tmp y se borra
# al acabar.
#
# Uso (como el usuario bitabit, nunca root):
#   test-backup.sh <binario bitabit para Linux>
#
# Sale con 0 solo si todo pasa.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
binary="${1:-}"
if [[ -z "$binary" || ! -x "$binary" ]]; then
	echo "Uso: $0 <binario bitabit para Linux, ejecutable>" >&2
	exit 2
fi
if [[ $EUID -eq 0 ]]; then
	echo "No se ejecuta como root." >&2
	exit 2
fi
for cmd in ffmpeg psql pg_restore rsync sha256sum; do
	command -v "$cmd" >/dev/null || { echo "Falta $cmd." >&2; exit 2; }
done

schema="bitabit_e2e_backup_$$"
work="$(mktemp -d /tmp/bitabit-backup-e2e.XXXXXX)"
pid=""
cleanup() {
	if [[ -n "$pid" ]]; then
		kill "$pid" 2>/dev/null || true
		wait "$pid" 2>/dev/null || true
	fi
	psql -d bitabit -qX -c "DROP SCHEMA IF EXISTS ${schema} CASCADE" -c "DROP SCHEMA IF EXISTS ${schema}_orig CASCADE" >/dev/null 2>&1 || true
	rm -rf -- "$work"
}
trap cleanup EXIT

passed=0
failed=0
check() {
	local what=$1
	shift
	if "$@"; then
		echo "  bien: $what"
		passed=$((passed + 1))
	else
		echo "  MAL:  $what" >&2
		failed=$((failed + 1))
	fi
}

data="$work/data"
backups="$work/copias"
mkdir -p "$data/entrada" "$data/biblioteca" "$data/fallidos"
install -d -m 0700 "$backups"
psql -d bitabit -qX -c "CREATE SCHEMA $schema" >/dev/null

# Archivos de configuración de prueba, con la clave de enlaces dentro: los scripts
# no deben cargarla.
env_file="$work/env"
backup_env_file="$work/backup.env"
url="postgres:///bitabit?host=/var/run/postgresql&options=-csearch_path%3D${schema}"
cat >"$env_file" <<EOF
BITABIT_DATABASE_URL=$url
BITABIT_DATA_DIR=$data
BITABIT_MEDIA_KEYS=k1:clave-de-prueba-que-no-debe-salir
EOF
cat >"$backup_env_file" <<EOF
BITABIT_BACKUP_DIR=$backups
BITABIT_BACKUP_KEEP_DAILY=7
BITABIT_BACKUP_KEEP_WEEKLY=4
EOF
export BITABIT_ENV_FILE="$env_file" BITABIT_BACKUP_ENV_FILE="$backup_env_file"

echo "== Datos: una pista de verdad, importada por el servidor"
(
	export BITABIT_DATABASE_URL="$url" BITABIT_DATA_DIR="$data" BITABIT_HTTP_ADDR=127.0.0.1:0
	export BITABIT_MEDIA_KEYS="k1:$(head -c 32 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=')"
	head -c 18 /dev/urandom | base64 | tr -d '+/=' | "$binary" password >/dev/null
)
ffmpeg -loglevel error -f lavfi -i "color=c=green:s=600x600" -frames:v 1 "$work/cover.png"
ffmpeg -loglevel error -f lavfi -i "sine=frequency=330:duration=3:sample_rate=48000" -i "$work/cover.png" \
	-map 0:a -map 1:v -ac 2 -c:a flac -c:v copy -disposition:v:0 attached_pic \
	-metadata title="Copia" -metadata artist="Prueba" -metadata album="Copias" "$work/src.flac"
cp "$work/src.flac" "$data/entrada/.t.flac"
mv "$data/entrada/.t.flac" "$data/entrada/copia.flac"
BITABIT_DATABASE_URL="$url" BITABIT_DATA_DIR="$data" BITABIT_HTTP_ADDR=127.0.0.1:0 \
	BITABIT_MEDIA_KEYS="k1:$(head -c 32 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=')" \
	"$binary" serve >"$work/serve.log" 2>&1 &
pid=$!
for _ in $(seq 1 90); do
	n="$(psql -d bitabit -AtX -c "SELECT count(*) FROM ${schema}.tracks" 2>/dev/null || echo 0)"
	[[ "$n" == 1 ]] && break
	sleep 2
done
kill "$pid"
wait "$pid" 2>/dev/null || true
pid=""
check "el servidor importó la pista" test "$n" = 1

echo "== Copia"
export BITABIT_BACKUP_TEST=1 BITABIT_BACKUP_TEST_SCHEMA="$schema"
check "backup-db.sh acaba bien" "$here/backup-db.sh"
check "backup-library.sh --verify acaba bien" "$here/backup-library.sh" --verify
dump="$(find "$backups/db" -maxdepth 1 -name 'bitabit-*.dump' | sort | tail -n 1)"
snapshot="$backups/library/$(readlink "$backups/library/latest" 2>/dev/null || echo ninguna)"
check "hay una copia de la base de datos" test -s "$dump"
check "hay una instantánea de la biblioteca con su original" test -n "$(find "$snapshot/orig" -type f -name '*.flac' 2>/dev/null)"
check "la copia de la base de datos es 0600" test "$(stat -c %a "$dump")" = 600
check "la clave de enlaces no está en ninguna parte de la copia" bash -c "! grep -rq 'clave-de-prueba-que-no-debe-salir' -- '$backups'"

echo "== Restauración de la biblioteca con restore.sh"
# restore.sh restaura la base en una base de datos que no sea «bitabit»; como el
# usuario bitabit no puede crear bases, aquí la base se restaura a mano (abajo) y de
# restore.sh se prueba la biblioteca y que se niega a lo que no debe.
check "restore.sh se niega a restaurar sobre /srv/bitabit" bash -c "! '$here/restore.sh' --copias '$backups' --base otra --datos /srv/bitabit 2>/dev/null"
check "restore.sh se niega a restaurar sobre la base bitabit" bash -c "! '$here/restore.sh' --copias '$backups' --base bitabit --datos '$work/x' 2>/dev/null"
check "restore.sh rechaza una fecha mal escrita" bash -c "! '$here/restore.sh' --copias '$backups' --fecha ayer --base otra --datos '$work/x' 2>/dev/null"
code=0
"$here/restore.sh" --copias "$work/no-hay" --base otra --datos "$work/x" >/dev/null 2>&1 || code=$?
check "restore.sh sin copias sale con 4" test "$code" = 4

restored="$work/restaurado"
mkdir -p "$restored/biblioteca"
for part in orig aac covers; do
	[[ -d "$snapshot/$part" ]] && rsync -a -- "$snapshot/$part/" "$restored/biblioteca/$part/"
done
check "la biblioteca restaurada es idéntica a la original" diff -r "$data/biblioteca/orig" "$restored/biblioteca/orig"
# Las carátulas solo existen con el PR de carátulas de la importación: si hay, se comparan.
if [[ -d "$data/biblioteca/covers" ]]; then
	check "las carátulas restauradas son idénticas" diff -r "$data/biblioteca/covers" "$restored/biblioteca/covers"
fi
check "el AAC restaurado es idéntico" diff -r "$data/biblioteca/aac" "$restored/biblioteca/aac"

echo "== Restauración de la base de datos en un esquema aparte"
psql -d bitabit -qX -c "ALTER SCHEMA ${schema} RENAME TO ${schema}_orig" >/dev/null
check "pg_restore de la copia" pg_restore --no-owner --no-privileges --exit-on-error -d bitabit -- "$dump"
same_rows() {
	local a b
	a="$(psql -d bitabit -AtX -c "SELECT md5(string_agg(t::text, '|' ORDER BY id)) FROM ${schema}_orig.tracks t")"
	b="$(psql -d bitabit -AtX -c "SELECT md5(string_agg(t::text, '|' ORDER BY id)) FROM ${schema}.tracks t")"
	[[ -n "$a" && "$a" == "$b" ]]
}
check "las pistas restauradas son las mismas" same_rows
hashes_match() {
	local id ext want got ok=0
	while IFS='|' read -r id ext want; do
		got="$(sha256sum -- "$restored/biblioteca/orig/$id.$ext" | cut -d' ' -f1)"
		[[ "$got" == "$want" ]] || ok=1
	done < <(psql -d bitabit -AtX -c "SELECT track_id, ext, encode(sha256, 'hex') FROM ${schema}.track_files WHERE quality = 'orig'")
	return "$ok"
}
check "los originales restaurados cuadran con los SHA-256 de la base restaurada" hashes_match

echo
echo "Resultado: $passed bien, $failed mal."
[[ $failed -eq 0 ]]
