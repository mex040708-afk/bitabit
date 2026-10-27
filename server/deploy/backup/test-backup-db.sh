#!/usr/bin/env bash
# Prueba reproducible de backup-db.sh (ADR-035 Copias de seguridad).
#
# No toca /srv/bitabit ni /etc/bitabit, ni los datos de la base bitabit: el
# usuario bitabit no puede crear bases, así que la prueba crea en ella un
# esquema temporal bitabit_backup_test_<pid>, lo copia con
# BITABIT_BACKUP_TEST_SCHEMA y lo borra al terminar. Las copias van a una
# carpeta temporal de /tmp.
#
# Uso (como el usuario bitabit, nunca root): bash test-backup-db.sh
# Variable opcional: BITABIT_TEST_DATABASE_URL (por defecto postgresql:///bitabit).
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
script="$here/backup-db.sh"
db_url="${BITABIT_TEST_DATABASE_URL:-postgresql:///bitabit}"
schema="bitabit_backup_test_$$"
schema_orig="${schema}_orig"
real_pg_restore="$(command -v pg_restore)"

if [[ $EUID -eq 0 ]]; then
	echo "No se ejecuta como root." >&2
	exit 1
fi

work="$(mktemp -d /tmp/bitabit-backup-test.XXXXXX)"
chmod 0700 -- "$work"

psql_q() {
	PGOPTIONS="-c client_min_messages=warning" psql --no-psqlrc --quiet --set=ON_ERROR_STOP=1 --dbname="$db_url" "$@"
}

cleanup() {
	psql_q -c "DROP SCHEMA IF EXISTS $schema CASCADE; DROP SCHEMA IF EXISTS $schema_orig CASCADE;" ||
		echo "aviso: no se pudieron borrar los esquemas temporales $schema*" >&2
	if [[ -n "$work" && "$work" == /tmp/bitabit-backup-test.* ]]; then
		rm -r -- "$work" || echo "aviso: no se pudo borrar $work" >&2
	fi
}
trap cleanup EXIT

passed=0
failed=0
ok() {
	passed=$((passed + 1))
	echo "  ok    $*"
}
fail() {
	failed=$((failed + 1))
	echo "  FALLA $*"
}
quiet() {
	"$@" >/dev/null 2>&1
}
check() {
	local desc="$1"
	shift
	if "$@"; then ok "$desc"; else fail "$desc"; fi
}

# Archivos de configuración de prueba: el script nunca lee /etc/bitabit.
env_file="$work/env"
backup_env_file="$work/backup.env"
data_dir="$work/data"
mkdir -m 0700 -- "$data_dir"
# La clave tiene una sustitución de órdenes: si el script hiciera source,
# crearía este archivo.
canary="$work/canary"
cat >"$env_file" <<EOF
BITABIT_DATABASE_URL="$db_url"
BITABIT_DATA_DIR='$data_dir'
BITABIT_MEDIA_KEYS=\$(touch $canary)
EOF
: >"$backup_env_file"

# Ejecuta el script con un entorno limpio; los argumentos extra son
# NOMBRE=valor. Deja el código de salida en $rc.
rc=0
run_backup() {
	rc=0
	env -i PATH="${TEST_PATH:-$PATH}" HOME="$HOME" LANG=C.UTF-8 \
		BITABIT_ENV_FILE="$env_file" BITABIT_BACKUP_ENV_FILE="$backup_env_file" \
		BITABIT_BACKUP_TEST=1 BITABIT_BACKUP_TEST_SCHEMA="$schema" \
		"$@" bash "$script" >"$work/out.log" 2>&1 || rc=$?
}

new_backup_dir() {
	local d="$work/$1"
	mkdir -m 0700 -- "$d"
	printf '%s' "$d"
}

list_dumps() {
	find "$1/db" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort
}

table_digest() {
	psql_q -At -c "SELECT count(*) || ':' || md5(string_agg(t::text, '|' ORDER BY t.id)) FROM $1.tracks t;"
}

echo "== Datos de prueba (esquema $schema)"
psql_q <<SQL
CREATE SCHEMA $schema;
CREATE TABLE $schema.artists (id bigserial PRIMARY KEY, name text NOT NULL);
CREATE TABLE $schema.tracks (
	id bigserial PRIMARY KEY,
	artist_id bigint NOT NULL REFERENCES $schema.artists(id),
	title text NOT NULL,
	sha256 bytea,
	added_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO $schema.artists (name) VALUES ('Artista uno'), ('Artista «dos»'), ('O''Brien');
INSERT INTO $schema.tracks (artist_id, title, sha256)
SELECT 1 + (g % 3), 'Pista ' || g || E' con\ttab y ñ', sha256(g::text::bytea)
FROM generate_series(1, 500) AS g;
SQL
before="$(table_digest "$schema")"

echo "== (1) Copia correcta"
dir1="$(new_backup_dir b1)"
run_backup BITABIT_BACKUP_DIR="$dir1" BITABIT_BACKUP_NOW=20261023T033000Z
dump1="$dir1/db/bitabit-20261023T033000Z.dump"
check "sale con 0 (salió $rc)" test "$rc" -eq 0
check "existe db/bitabit-20261023T033000Z.dump" test -f "$dump1"
check "la copia tiene modo 0600" test "$(stat -c %a -- "$dump1" 2>/dev/null)" = 600
check "db/ tiene modo 0700" test "$(stat -c %a -- "$dir1/db" 2>/dev/null)" = 700
check "no queda ningún .partial" test -z "$(find "$dir1/db" -name '*.partial')"
check "lee la configuración sin ejecutarla (no hay canario)" test ! -e "$canary"
check "los mensajes no llevan la URL de la base" bash -c "! grep -qF -- '$db_url' '$work/out.log'"

echo "== (2) Restauración en otro esquema"
psql_q -c "ALTER SCHEMA $schema RENAME TO $schema_orig;"
if "$real_pg_restore" --no-owner --no-privileges --single-transaction --exit-on-error \
	--dbname="$db_url" -- "$dump1"; then
	ok "pg_restore de la copia"
else
	fail "pg_restore de la copia"
fi
after="$(table_digest "$schema" 2>/dev/null || echo error)"
orig="$(table_digest "$schema_orig")"
check "mismas filas tras restaurar ($after)" test "$after" = "$before"
check "el original sigue igual" test "$orig" = "$before"
check "mismos artistas" test \
	"$(psql_q -At -c "SELECT string_agg(name, ',' ORDER BY id) FROM $schema.artists")" = \
	"$(psql_q -At -c "SELECT string_agg(name, ',' ORDER BY id) FROM $schema_orig.artists")"
# El esquema restaurado sigue siendo el que se copia en las pruebas siguientes.
psql_q -c "DROP SCHEMA $schema_orig CASCADE;"

echo "== (3a) Retención con copias antiguas falsas (7 diarias, 4 semanales)"
dir3="$(new_backup_dir b3)"
mkdir -m 0700 -- "$dir3/db"
# Una copia falsa cada día, del 1 de septiembre al 22 de octubre de 2026.
for i in $(seq 0 51); do
	d="$(date -u -d "2026-09-01 +$i days" +%Y%m%d)"
	echo falsa >"$dir3/db/bitabit-${d}T033000Z.dump"
done
echo "no es una copia" >"$dir3/db/notas.txt"
echo "no es una copia" >"$dir3/db/bitabit-2026.dump"
echo "no es una copia" >"$dir3/db/bitabit-20260901T033000Z.dump.bak"
echo "de otro proceso" >"$dir3/db/.bitabit-20260901T040000Z.dump.partial"
run_backup BITABIT_BACKUP_DIR="$dir3" BITABIT_BACKUP_NOW=20261023T033000Z
check "sale con 0 (salió $rc)" test "$rc" -eq 0
# El 23 de octubre de 2026 es viernes. Diarias: del 17 al 23. Semanales (la
# más reciente de cada semana ISO): 23 (semana 43), 18 (42), 11 (41), 4 (40).
expected3="$(printf '%s\n' \
	.bitabit-20260901T040000Z.dump.partial \
	bitabit-2026.dump \
	bitabit-20260901T033000Z.dump.bak \
	bitabit-20261004T033000Z.dump \
	bitabit-20261011T033000Z.dump \
	bitabit-20261017T033000Z.dump \
	bitabit-20261018T033000Z.dump \
	bitabit-20261019T033000Z.dump \
	bitabit-20261020T033000Z.dump \
	bitabit-20261021T033000Z.dump \
	bitabit-20261022T033000Z.dump \
	bitabit-20261023T033000Z.dump \
	notas.txt | LC_ALL=C sort)"
got3="$(list_dumps "$dir3")"
if [[ "$got3" == "$expected3" ]]; then
	ok "quedan exactamente las esperadas"
else
	fail "quedan exactamente las esperadas"
	diff <(echo "$expected3") <(echo "$got3") || true
fi

echo "== (3b) Retención a lo largo de diez noches (2 diarias, 2 semanales)"
dir4="$(new_backup_dir b4)"
all_ok=1
for day in 14 15 16 17 18 19 20 21 22 23; do
	run_backup BITABIT_BACKUP_DIR="$dir4" BITABIT_BACKUP_NOW="202610${day}T033000Z" \
		BITABIT_BACKUP_KEEP_DAILY=2 BITABIT_BACKUP_KEEP_WEEKLY=2
	if [[ "$rc" -ne 0 ]]; then
		all_ok=0
		echo "    la noche del $day salió con $rc"
	fi
done
check "las diez noches salen con 0" test "$all_ok" -eq 1
# Diarias 22 y 23; semanales 23 (semana 43) y 18, el domingo de la 42.
expected4="$(printf '%s\n' bitabit-20261018T033000Z.dump bitabit-20261022T033000Z.dump bitabit-20261023T033000Z.dump)"
got4="$(list_dumps "$dir4")"
if [[ "$got4" == "$expected4" ]]; then
	ok "quedan 18, 22 y 23"
else
	fail "quedan 18, 22 y 23"
	diff <(echo "$expected4") <(echo "$got4") || true
fi

echo "== (3c) Configuración leída del archivo, con comillas"
dir5="$(new_backup_dir b5)"
mkdir -m 0700 -- "$dir5/db"
for d in 20261020 20261021 20261022; do echo falsa >"$dir5/db/bitabit-${d}T033000Z.dump"; done
printf '%s\n' "# comentario" "BITABIT_BACKUP_DIR=\"$dir5\"" "export BITABIT_BACKUP_KEEP_DAILY='1'" \
	"BITABIT_BACKUP_KEEP_WEEKLY=1" >"$backup_env_file"
run_backup BITABIT_BACKUP_NOW=20261023T033000Z
check "sale con 0 (salió $rc)" test "$rc" -eq 0
check "con 1 y 1 solo queda la nueva" test "$(list_dumps "$dir5")" = "bitabit-20261023T033000Z.dump"
: >"$backup_env_file"

echo "== (4) Fallos"
run_backup BITABIT_BACKUP_NOW=20261023T033000Z
check "sin BITABIT_BACKUP_DIR sale con 2 (salió $rc)" test "$rc" -eq 2

run_backup BITABIT_BACKUP_DIR="relativa/copias"
check "ruta relativa sale con 2 (salió $rc)" test "$rc" -eq 2

run_backup BITABIT_BACKUP_DIR="$work/no-existe"
check "carpeta inexistente sale con 2 (salió $rc)" test "$rc" -eq 2

inside="$data_dir/copias"
mkdir -m 0700 -- "$inside"
run_backup BITABIT_BACKUP_DIR="$inside"
check "carpeta dentro de la de datos sale con 2 (salió $rc)" test "$rc" -eq 2
run_backup BITABIT_BACKUP_DIR="$data_dir"
check "carpeta igual a la de datos sale con 2 (salió $rc)" test "$rc" -eq 2
check "no se creó db/ dentro de la carpeta de datos" test ! -e "$inside/db"

open_dir="$work/abierta"
mkdir -m 0755 -- "$open_dir"
run_backup BITABIT_BACKUP_DIR="$open_dir"
check "modo 0755 sale con 2 (salió $rc)" test "$rc" -eq 2

dir6="$(new_backup_dir b6)"
run_backup BITABIT_BACKUP_DIR="$dir6" BITABIT_BACKUP_KEEP_DAILY=0
check "KEEP_DAILY=0 sale con 2 (salió $rc)" test "$rc" -eq 2
run_backup BITABIT_BACKUP_DIR="$dir6" BITABIT_BACKUP_KEEP_WEEKLY=dos
check "KEEP_WEEKLY no numérico sale con 2 (salió $rc)" test "$rc" -eq 2
run_backup BITABIT_BACKUP_DIR="$dir6" BITABIT_DATABASE_URL=
check "BITABIT_DATABASE_URL vacía sale con 2 (salió $rc)" test "$rc" -eq 2

run_backup BITABIT_BACKUP_DIR="$dir6" BITABIT_BACKUP_NOW=20261023T033000Z \
	BITABIT_DATABASE_URL="postgresql:///bitabit_backup_test_no_existe_$$"
check "base inexistente sale con 3 (salió $rc)" test "$rc" -eq 3
check "base inexistente no deja .partial ni copia" test -z "$(list_dumps "$dir6")"

fake_bin="$work/fake-bin"
mkdir -m 0700 -- "$fake_bin"
printf '#!/bin/sh\necho "pg_restore falso: archivo dañado" >&2\nexit 1\n' >"$fake_bin/pg_restore"
chmod 0700 -- "$fake_bin/pg_restore"
dir7="$(new_backup_dir b7)"
TEST_PATH="$fake_bin:$PATH" run_backup BITABIT_BACKUP_DIR="$dir7" BITABIT_BACKUP_NOW=20261023T033000Z
check "verificación fallida sale con 4 (salió $rc)" test "$rc" -eq 4
check "verificación fallida no deja .partial ni copia" test -z "$(list_dumps "$dir7")"

run_backup BITABIT_BACKUP_DIR="$dir1" BITABIT_BACKUP_NOW=20261023T033000Z
check "no pisa una copia existente con la misma fecha (salió $rc)" test "$rc" -eq 3
check "la copia existente sigue entera" quiet "$real_pg_restore" --list -- "$dump1"

check "--help sale con 0" quiet bash "$script" --help

echo
echo "Resumen: $passed bien, $failed mal."
if ((failed > 0)); then
	exit 1
fi
exit 0
