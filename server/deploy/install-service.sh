#!/usr/bin/env bash
# Instala o actualiza el binario bitabit y su servicio de systemd, y lo
# (re)arranca. Antes hay que haber ejecutado setup-server.sh.
#
# Uso: sudo ./install-service.sh <binario compilado para Linux>
#      (en Windows: GOOS=linux GOARCH=amd64 go build -o bitabit ./cmd/server)
set -euo pipefail

binary="${1:-}"
here="$(cd "$(dirname "$0")" && pwd)"

if [[ $EUID -ne 0 ]]; then
	echo "Hay que ejecutarlo como root (sudo)." >&2
	exit 1
fi
if [[ -z "$binary" || ! -f "$binary" ]]; then
	echo "Uso: sudo $0 <binario compilado para Linux>" >&2
	exit 1
fi
if [[ ! -f /etc/bitabit/env ]]; then
	echo "Falta /etc/bitabit/env: ejecuta antes setup-server.sh." >&2
	exit 1
fi

echo "== Archivos"
install -m 0755 -o root -g root "$binary" /usr/local/bin/bitabit
install -m 0644 -o root -g root "$here/bitabit.service" /etc/systemd/system/bitabit.service
systemd-analyze verify /etc/systemd/system/bitabit.service

echo "== Servicio"
systemctl daemon-reload
systemctl enable bitabit.service
systemctl restart bitabit.service
# Type=exec ya avisa si no arranca; esperar un poco descubre si se cae al
# conectar con la base de datos o al abrir el puerto.
sleep 3
if ! systemctl is-active --quiet bitabit.service; then
	echo "El servicio no sigue activo. Últimas líneas del registro:" >&2
	journalctl -u bitabit.service -n 20 --no-pager >&2
	exit 1
fi
systemctl --no-pager --lines=5 status bitabit.service
echo "Listo."
