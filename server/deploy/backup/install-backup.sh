#!/usr/bin/env bash
# Instala o actualiza las copias de seguridad de Bitabit (ADR-035): los scripts en
# /usr/local/lib/bitabit/backup, la unidad y el temporizador de systemd, y la
# carpeta de copias. Antes hay que haber ejecutado setup-server.sh.
#
# Uso: sudo ./install-backup.sh <carpeta de copias>
#      La carpeta debe ser absoluta, estar en otro disco y fuera de /srv/bitabit,
#      /home y /root (la unidad no ve /home). Si no existe, se crea.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
lib=/usr/local/lib/bitabit/backup
service_user=bitabit
data_dir=/srv/bitabit
backup_env=/etc/bitabit/backup.env
dropin_dir=/etc/systemd/system/bitabit-backup.service.d

if [[ $EUID -ne 0 ]]; then
	echo "Hay que ejecutarlo como root (sudo)." >&2
	exit 1
fi
if [[ $# -ne 1 ]]; then
	echo "Uso: sudo $0 <carpeta de copias>" >&2
	exit 1
fi
if [[ ! -d /etc/bitabit || ! -f /etc/bitabit/env ]]; then
	echo "Falta /etc/bitabit/env: ejecuta antes setup-server.sh." >&2
	exit 1
fi

# Ruta normalizada, sin crearla todavía, para validarla antes de tocar nada.
dir="$(realpath -m -- "$1")"
case "$dir" in
/ | "$data_dir" | "$data_dir"/* | /home | /home/* | /root | /root/* | /etc | /etc/* | /usr | /usr/*)
	echo "La carpeta de copias no puede ser $dir: tiene que estar fuera de $data_dir, /home, /root, /etc y /usr, a ser posible en otro disco." >&2
	exit 1
	;;
esac
for f in backup-db.sh backup-library.sh bitabit-backup.service bitabit-backup.timer; do
	if [[ ! -f "$here/$f" ]]; then
		echo "Falta $here/$f." >&2
		exit 1
	fi
done
for cmd in pg_dump pg_restore psql rsync sha256sum; do
	if ! command -v "$cmd" >/dev/null; then
		echo "Falta $cmd: instala postgresql-client y rsync (setup-server.sh lo hace)." >&2
		exit 1
	fi
done

echo "== Carpeta de copias"
install -d -o "$service_user" -g "$service_user" -m 0700 "$dir"
chown "$service_user:$service_user" "$dir"
chmod 0700 "$dir"

echo "== Configuración"
# Como /etc/bitabit/env, nunca se pisa: si ya existe y apunta a otra carpeta, se avisa.
if [[ -f "$backup_env" ]]; then
	if ! grep -qx "BITABIT_BACKUP_DIR=$dir" "$backup_env"; then
		echo "Ya existe $backup_env con otra carpeta; no se toca. Cámbiala a mano si hace falta." >&2
	fi
else
	umask 027
	cat >"$backup_env" <<EOF
# Copias de seguridad de Bitabit (ADR-035). Lo leen los scripts de $lib.
BITABIT_BACKUP_DIR=$dir
BITABIT_BACKUP_KEEP_DAILY=7
BITABIT_BACKUP_KEEP_WEEKLY=4
EOF
	chown root:"$service_user" "$backup_env"
	chmod 0640 "$backup_env"
fi
# La carpeta que de verdad usan los scripts es la del archivo, aunque no sea la
# que se pasó: la unidad tiene que poder escribir en ella.
configured="$(sed -n 's/^BITABIT_BACKUP_DIR=//p' "$backup_env" | tail -n 1)"
configured="${configured%\"}"
configured="${configured#\"}"
if [[ -z "$configured" || "$configured" != /* ]]; then
	echo "$backup_env no tiene una BITABIT_BACKUP_DIR absoluta." >&2
	exit 1
fi

echo "== Archivos"
install -d -o root -g root -m 0755 "$lib"
install -m 0755 -o root -g root "$here/backup-db.sh" "$lib/backup-db.sh"
install -m 0755 -o root -g root "$here/backup-library.sh" "$lib/backup-library.sh"
install -m 0644 -o root -g root "$here/bitabit-backup.service" /etc/systemd/system/bitabit-backup.service
install -m 0644 -o root -g root "$here/bitabit-backup.timer" /etc/systemd/system/bitabit-backup.timer
install -d -o root -g root -m 0755 "$dropin_dir"
printf '# Lo escribe install-backup.sh: la carpeta de copias es lo único escribible.\n[Service]\nReadWritePaths=%s\n' "$configured" >"$dropin_dir/backup-dir.conf"
chmod 0644 "$dropin_dir/backup-dir.conf"
systemd-analyze verify /etc/systemd/system/bitabit-backup.service /etc/systemd/system/bitabit-backup.timer

echo "== Temporizador"
systemctl daemon-reload
systemctl enable --now bitabit-backup.timer
systemctl --no-pager list-timers bitabit-backup.timer
echo "Listo. Para una copia ahora: sudo systemctl start bitabit-backup.service"
echo "Para ver cómo fue: journalctl -u bitabit-backup -n 50"
