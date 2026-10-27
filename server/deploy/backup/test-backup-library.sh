#!/usr/bin/env bash
# Prueba de backup-library.sh (pieza b del ADR-035 Copias de seguridad).
#
# Todo ocurre en una carpeta nueva de /tmp con archivos de bytes aleatorios: no
# toca /srv/bitabit ni /etc/bitabit. Para --verify crea una base temporal
# bitabit_lib_test_<pid>; si el usuario no puede crear bases, usa un esquema
# temporal con ese nombre dentro de la base bitabit (solo crea y borra ese
# esquema). Se ejecuta como el usuario bitabit, nunca como root.
#
# Uso: test-backup-library.sh      (sale 0 solo si pasan todas las pruebas)
set -euo pipefail

script="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/backup-library.sh"
if [[ $EUID -eq 0 ]]; then
	echo "No se ejecuta como root." >&2
	exit 1
fi

work=$(mktemp -d /tmp/bitabit-lib-test.XXXXXX)
chmod 0700 "$work"
test_db="bitabit_lib_test_$$"
db_mode=""

cleanup() {
	case $db_mode in
	db)
		dropdb --if-exists "$test_db" || echo "no se pudo borrar la base $test_db" >&2
		;;
	schema)
		psql --dbname=bitabit -qAtX -v ON_ERROR_STOP=1 -c "SET client_min_messages = warning; DROP SCHEMA IF EXISTS $test_db CASCADE" ||
			echo "no se pudo borrar el esquema $test_db" >&2
		;;
	esac
	rm -rf -- "$work"
}
trap cleanup EXIT

passed=0
failed=0
ok() {
	passed=$((passed + 1))
	printf 'ok    %s\n' "$*"
}
ko() {
	failed=$((failed + 1))
	printf 'FALLA %s\n' "$*"
}
check() {
	local what=$1
	shift
	if "$@"; then ok "$what"; else ko "$what"; fi
}

data="$work/data"
lib="$data/biblioteca"
backup="$work/backup"
secret="clave-que-no-debe-salir-$$"
mkdir -p "$lib/orig" "$lib/aac" "$lib/covers/thumbs" "$lib/.tmp-123" \
	"$data/entrada" "$data/fallidos" "$work/outside" "$backup"
chmod 0700 "$backup"

rand() { head -c "$2" /dev/urandom >"$1"; }
rand "$lib/orig/1.flac" 4096
rand "$lib/orig/2.flac" 8192
rand "$lib/aac/1.m4a" 2048
rand "$lib/aac/2.m4a" 2048
rand "$lib/covers/1.jpg" 1024
rand "$lib/covers/thumbs/1-256.jpg" 512
rand "$lib/.tmp-123/x" 100
rand "$lib/otro.txt" 100
rand "$data/entrada/a.flac" 100
rand "$data/fallidos/b.flac" 100
rand "$work/outside/secreto" 100
chmod 0644 "$lib/orig/1.flac"
ln -s "$work/outside/secreto" "$lib/orig/9.flac"

db_url="dbname=$test_db"
cat >"$work/env" <<EOF
# Archivo de entorno de prueba
BITABIT_DATA_DIR=$data
BITABIT_DATABASE_URL='$db_url'
BITABIT_MEDIA_KEYS=$secret
BITABIT_LISTEN=127.0.0.1:8080
EOF
cat >"$work/backup.env" <<EOF
BITABIT_BACKUP_DIR="$backup"
BITABIT_BACKUP_KEEP_DAILY=7
BITABIT_BACKUP_KEEP_WEEKLY=4
EOF

# run FECHA [VAR=valor…] [-- argumentos]: ejecuta la copia con un entorno limpio
# de variables BITABIT_* y deja el código en $rc y la salida en $out.
rc=0
out=""
run() {
	local now=$1
	shift
	local extra=()
	while [[ $# -gt 0 && "$1" != -- ]]; do
		extra+=("$1")
		shift
	done
	[[ "${1:-}" == -- ]] && shift
	set +e
	out=$(env -u BITABIT_DATA_DIR -u BITABIT_DATABASE_URL -u BITABIT_MEDIA_KEYS \
		-u BITABIT_BACKUP_DIR -u BITABIT_BACKUP_KEEP_DAILY -u BITABIT_BACKUP_KEEP_WEEKLY \
		BITABIT_ENV_FILE="$work/env" BITABIT_BACKUP_ENV_FILE="$work/backup.env" \
		BITABIT_BACKUP_TEST=1 BITABIT_BACKUP_NOW="$now" "${extra[@]}" \
		bash "$script" "$@" 2>&1)
	rc=$?
	set -e
	if [[ "$out" == *"$secret"* || "$out" == *"$db_url"* ]]; then
		ko "la salida de $now no muestra la clave ni la URL de la base"
	fi
	all_out+="$out"$'\n'
}
all_out=""
expect_rc() {
	local want=$1 what=$2
	if [[ $rc -eq $want ]]; then
		ok "$what (código $rc)"
	else
		ko "$what: código $rc, se esperaba $want"
		printf '%s\n' "$out" | sed 's/^/      | /'
	fi
}
no_partial() { ! compgen -G "$1/library/.*.partial" >/dev/null; }
latest_is() { [[ "$(readlink "$1/library/latest")" == "$2" ]]; }
perms_ok() {
	local snap=$1
	[[ "$(stat -c %a "$backup/library")" == 700 ]] &&
		[[ -z "$(find "$snap" -type d ! -perm 0700)" ]] &&
		[[ -z "$(find "$snap" -type f ! -perm 0600)" ]]
}
same_inode() { [[ "$(stat -c %i "$1")" == "$(stat -c %i "$2")" ]]; }

echo "== 1. Primera copia"
s1=20261020T033000Z
run "$s1"
expect_rc 0 "primera copia"
snap1="$backup/library/$s1"
check "trae orig, aac y covers" test -f "$snap1/orig/1.flac" -a -f "$snap1/orig/2.flac" -a \
	-f "$snap1/aac/1.m4a" -a -f "$snap1/covers/1.jpg" -a -f "$snap1/covers/thumbs/1-256.jpg"
check "el contenido es idéntico" cmp -s "$lib/orig/2.flac" "$snap1/orig/2.flac"
check "latest apunta a la copia" latest_is "$backup" "$s1"
check "permisos 0700/0600" perms_ok "$snap1"
check "no copia .tmp-*, otro.txt, entrada/ ni fallidos/" bash -c \
	"[[ -z \"\$(find '$backup' -name '.tmp-*' -o -name otro.txt -o -name a.flac -o -name b.flac -o -name entrada -o -name fallidos)\" ]]"
check "no sigue ni copia el enlace simbólico de orig/" test ! -e "$snap1/orig/9.flac" -a ! -L "$snap1/orig/9.flac"
check "no deja .partial" no_partial "$backup"

echo "== 2. Segunda copia: enlaces duros"
rand "$lib/orig/3.flac" 4096
s2=20261021T033000Z
run "$s2"
expect_rc 0 "segunda copia"
snap2="$backup/library/$s2"
check "orig/1.flac comparte inodo con la primera" same_inode "$snap1/orig/1.flac" "$snap2/orig/1.flac"
check "aac/2.m4a comparte inodo con la primera" same_inode "$snap1/aac/2.m4a" "$snap2/aac/2.m4a"
check "covers/thumbs comparte inodo con la primera" same_inode "$snap1/covers/thumbs/1-256.jpg" "$snap2/covers/thumbs/1-256.jpg"
check "el archivo nuevo aparece en la segunda" test -f "$snap2/orig/3.flac"
check "y no en la primera" test ! -e "$snap1/orig/3.flac"
check "latest apunta a la segunda" latest_is "$backup" "$s2"

echo "== 3. --verify"
psql_test() { psql --dbname="$db_url" -qAtX -v ON_ERROR_STOP=1 "$@"; }
if createdb "$test_db" 2>/dev/null; then
	db_mode=db
	echo "   (base temporal $test_db)"
else
	# Sin permiso para crear bases: esquema temporal en bitabit y search_path
	# en la cadena de conexión.
	psql --dbname=bitabit -qAtX -v ON_ERROR_STOP=1 -c "CREATE SCHEMA $test_db"
	db_mode=schema
	db_url="dbname=bitabit options=-csearch_path=$test_db"
	sed -i "s|^BITABIT_DATABASE_URL=.*|BITABIT_DATABASE_URL='$db_url'|" "$work/env"
	echo "   (sin permiso para crear bases: esquema temporal $test_db en bitabit)"
fi
psql_test -c "CREATE TABLE track_files (
	track_id bigint NOT NULL,
	quality text NOT NULL,
	ext text NOT NULL,
	sha256 bytea NOT NULL,
	bytes bigint NOT NULL,
	PRIMARY KEY (track_id, quality))"
add_row() {
	local id=$1 quality=$2 file=$3 ext=${3##*.} hex bytes
	hex=$(sha256sum "$file" | cut -d' ' -f1)
	bytes=$(stat -c %s "$file")
	psql_test -c "INSERT INTO track_files VALUES ($id, '$quality', '$ext', decode('$hex', 'hex'), $bytes)"
}
add_row 1 orig "$lib/orig/1.flac"
add_row 2 orig "$lib/orig/2.flac"
add_row 3 orig "$lib/orig/3.flac"
add_row 1 aac256 "$lib/aac/1.m4a"
add_row 2 aac256 "$lib/aac/2.m4a"

s3=20261022T033000Z
run "$s3" -- --verify
expect_rc 0 "--verify con los hashes buenos"
check "latest apunta a la verificada" latest_is "$backup" "$s3"
check "con --verify sigue enlazando lo que no cambia" same_inode "$snap2/orig/1.flac" "$backup/library/$s3/orig/1.flac"

# Como la corrupción silenciosa de un disco: cambia el contenido, no el tamaño
# ni la fecha de modificación, así que la comprobación rápida de rsync no la ve.
cp -p "$lib/orig/2.flac" "$work/2.flac.bak"
byte=$(od -An -tu1 -j100 -N1 "$lib/orig/2.flac" | tr -d ' ')
printf "\\$(printf '%03o' $(((byte + 1) % 256)))" | dd of="$lib/orig/2.flac" bs=1 seek=100 conv=notrunc status=none
touch -r "$work/2.flac.bak" "$lib/orig/2.flac"
s4=20261023T033000Z
run "$s4" -- --verify
expect_rc 4 "--verify con un byte cambiado en un original"
check "no guarda la copia mala" test ! -e "$backup/library/$s4"
check "no deja .partial" no_partial "$backup"
check "latest sigue en la buena" latest_is "$backup" "$s3"
cp -p "$work/2.flac.bak" "$lib/orig/2.flac"

mv "$lib/orig/3.flac" "$work/3.flac.bak"
run "$s4" -- --verify
expect_rc 4 "--verify con un original que falta"
check "no deja .partial" no_partial "$backup"
check "latest sigue en la buena" latest_is "$backup" "$s3"
mv "$work/3.flac.bak" "$lib/orig/3.flac"

run "$s4" -- --verify
expect_rc 0 "--verify otra vez bien tras arreglarlo"

echo "== 4. Retención"
# Semanas ISO de 2026: el 26 de octubre es lunes (semana 44).
retention_case() {
	local dir=$1
	rm -rf "$dir"
	mkdir -m 0700 "$dir" "$dir/library"
	shift
	local name
	for name in "$@"; do
		mkdir -p "$dir/library/$name/orig"
	done
}
has() { test -d "$1/library/$2"; }
hasnt() { test ! -e "$1/library/$2"; }

ra="$work/backup-a"
retention_case "$ra" \
	20261025T033000Z 20261024T033000Z 20261023T033000Z 20261018T033000Z \
	20261017T033000Z 20261011T033000Z 20261004T033000Z 20260927T033000Z \
	otra-cosa 20261001T000000Z.old 2026101T000000Z .20261001T000000Z.partial 20261399T000000Z
echo x >"$ra/library/20261002T000000Z"
mkdir -p "$work/outside/snap"
echo x >"$work/outside/snap/keep"
ln -s "$work/outside/snap" "$ra/library/20260101T000000Z"
run 20261026T033000Z BITABIT_BACKUP_DIR="$ra" BITABIT_BACKUP_KEEP_DAILY=3 BITABIT_BACKUP_KEEP_WEEKLY=4
expect_rc 0 "copia con retención 3 diarias y 4 semanales"
for s in 20261026T033000Z 20261025T033000Z 20261024T033000Z 20261018T033000Z 20261011T033000Z; do
	check "queda $s" has "$ra" "$s"
done
for s in 20261023T033000Z 20261017T033000Z 20261004T033000Z 20260927T033000Z; do
	check "se borra $s" hasnt "$ra" "$s"
done
for s in otra-cosa 20261001T000000Z.old 2026101T000000Z .20261001T000000Z.partial 20261399T000000Z; do
	check "no toca $s" has "$ra" "$s"
done
check "no toca un archivo con nombre de copia" test -f "$ra/library/20261002T000000Z"
check "no toca un enlace con nombre de copia ni su destino" test -L "$ra/library/20260101T000000Z" -a -f "$work/outside/snap/keep"
check "latest apunta a la nueva" latest_is "$ra" 20261026T033000Z

# La copia nueva es más antigua que otras (reloj atrasado): sin la protección
# de latest, con 1 diaria y 1 semanal, se borraría.
rb="$work/backup-b"
retention_case "$rb" 20261231T033000Z 20261230T033000Z
run 20261026T033000Z BITABIT_BACKUP_DIR="$rb" BITABIT_BACKUP_KEEP_DAILY=1 BITABIT_BACKUP_KEEP_WEEKLY=1
expect_rc 0 "copia con retención 1 y 1 y copias más nuevas"
check "latest nunca se borra" has "$rb" 20261026T033000Z
check "latest apunta a la nueva" latest_is "$rb" 20261026T033000Z
check "queda la más reciente" has "$rb" 20261231T033000Z
check "se borra la que sobra" hasnt "$rb" 20261230T033000Z

echo "== 5. Fallos"
grep -v '^BITABIT_BACKUP_DIR=' "$work/backup.env" >"$work/backup-sin-dir.env"
run 20261027T033000Z BITABIT_BACKUP_ENV_FILE="$work/backup-sin-dir.env"
expect_rc 2 "sin BITABIT_BACKUP_DIR"

run 20261027T033000Z BITABIT_BACKUP_DIR=relativa/copia
expect_rc 2 "BITABIT_BACKUP_DIR relativa"

run 20261027T033000Z BITABIT_BACKUP_DIR="$work/no-existe"
expect_rc 2 "BITABIT_BACKUP_DIR que no existe"

mkdir -m 0700 "$data/copia"
chmod 0700 "$data" # que falle por estar dentro, no por el modo
run 20261027T033000Z BITABIT_BACKUP_DIR="$data/copia"
expect_rc 2 "BITABIT_BACKUP_DIR dentro de la carpeta de datos"
run 20261027T033000Z BITABIT_BACKUP_DIR="$data"
expect_rc 2 "BITABIT_BACKUP_DIR igual a la carpeta de datos"
ln -s "$data/copia" "$work/enlace-a-datos"
run 20261027T033000Z BITABIT_BACKUP_DIR="$work/enlace-a-datos"
expect_rc 2 "BITABIT_BACKUP_DIR que es un enlace a la carpeta de datos"
check "no escribe en la carpeta de datos" test -z "$(ls -A "$data/copia")"

mkdir -m 0755 "$work/abierta"
run 20261027T033000Z BITABIT_BACKUP_DIR="$work/abierta"
expect_rc 2 "BITABIT_BACKUP_DIR con modo 0755"

run 20261027T033000Z BITABIT_BACKUP_KEEP_DAILY=0
expect_rc 2 "BITABIT_BACKUP_KEEP_DAILY=0"
run 20261027T033000Z BITABIT_BACKUP_KEEP_WEEKLY=x
expect_rc 2 "BITABIT_BACKUP_KEEP_WEEKLY no numérico"

run 20261027T033000Z -- --nada
expect_rc 2 "argumento desconocido"

mkdir -p "$work/fakebin"
printf '#!/bin/sh\nexit 23\n' >"$work/fakebin/rsync"
chmod 0755 "$work/fakebin/rsync"
run 20261027T033000Z PATH="$work/fakebin:$PATH"
expect_rc 3 "rsync que falla"
check "no deja .partial" no_partial "$backup"
check "no crea la copia" test ! -e "$backup/library/20261027T033000Z"
check "latest no cambia" latest_is "$backup" "$s4"

run 20261027T033000Z -- --help
expect_rc 0 "--help"

check "ninguna salida muestra la clave de enlaces" test "${all_out//$secret/}" == "$all_out"

if command -v shellcheck >/dev/null 2>&1; then
	check "shellcheck" shellcheck "$script" "${BASH_SOURCE[0]}"
else
	echo "   (shellcheck no está instalado; se salta)"
fi

echo
echo "Pasan $passed, fallan $failed."
[[ $failed -eq 0 ]]
