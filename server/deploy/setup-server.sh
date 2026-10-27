#!/usr/bin/env bash
# Prepara un Ubuntu 24.04 para ejecutar Bitabit: paquetes, usuario de
# servicio, carpetas de datos, base de datos y /etc/bitabit/env. Se puede repetir
# sin romper nada. Después, install-service.sh instala el binario y el servicio.
#
# Uso: sudo ./setup-server.sh [carpeta de datos]   (por defecto /srv/bitabit)
#
# Necesita systemd en marcha, para arrancar PostgreSQL.
set -euo pipefail

data_dir="${1:-/srv/bitabit}"
service_user=bitabit
database=bitabit

if [[ $EUID -ne 0 ]]; then
	echo "Hay que ejecutarlo como root (sudo)." >&2
	exit 1
fi
if [[ "$data_dir" != /* ]]; then
	echo "La carpeta de datos tiene que ser una ruta absoluta: $data_dir" >&2
	exit 1
fi

echo "== Paquetes"
export DEBIAN_FRONTEND=noninteractive
apt-get update -q
apt-get install -y -q --no-install-recommends postgresql ffmpeg flac rsync

echo "== Usuario $service_user"
# De sistema y con la contraseña bloqueada: nadie entra con él por SSH ni
# tiene sudo. Lleva bash para poder ser el usuario por defecto en WSL.
if ! id "$service_user" >/dev/null 2>&1; then
	useradd --system --create-home --home-dir "/var/lib/$service_user" \
		--shell /bin/bash "$service_user"
fi

echo "== Carpetas en $data_dir"
# entrada/ la llena el desarrollador; biblioteca/ y fallidos/, el servicio.
# Todas en el mismo sistema de archivos para que mover sea atómico.
for dir in "$data_dir" "$data_dir/entrada" "$data_dir/biblioteca" "$data_dir/fallidos"; do
	install -d -o "$service_user" -g "$service_user" -m 0750 "$dir"
done

echo "== PostgreSQL"
systemctl enable --now postgresql
# Rol y base con el nombre del usuario del sistema: Ubuntu autentica por
# socket con «peer», así que no hay contraseña de base de datos que guardar.
psql_postgres() {
	runuser -u postgres -- psql -v ON_ERROR_STOP=1 -tAc "$1"
}
if [[ "$(psql_postgres "SELECT 1 FROM pg_roles WHERE rolname = '$service_user'")" != 1 ]]; then
	runuser -u postgres -- createuser "$service_user"
fi
if [[ "$(psql_postgres "SELECT 1 FROM pg_database WHERE datname = '$database'")" != 1 ]]; then
	runuser -u postgres -- createdb --owner="$service_user" "$database"
fi

echo "== Configuración en /etc/bitabit/env"
# Fuera del repositorio. Solo se crea si no existe: así nunca se pisa una
# clave en uso, que invalidaría los enlaces repartidos. La clave no se muestra.
install -d -o root -g "$service_user" -m 0750 /etc/bitabit
if [[ -e /etc/bitabit/env ]]; then
	echo "ya existe: no se toca"
else
	key="$(head -c 32 /dev/urandom | basenc --base64url | tr -d '=')"
	(
		umask 0027
		# La cadena de conexión va en forma URL, sin espacios, para que el
		# archivo se pueda cargar igual desde systemd que desde una shell.
		cat >/etc/bitabit/env <<EOF
BITABIT_DATABASE_URL=postgres:///$database?host=/var/run/postgresql
BITABIT_DATA_DIR=$data_dir
BITABIT_MEDIA_KEYS=k1:$key
BITABIT_HTTP_ADDR=127.0.0.1:8080
EOF
	)
	chown root:"$service_user" /etc/bitabit/env
	chmod 0640 /etc/bitabit/env
	echo "creada, con una clave nueva"
fi

echo "== Comprobación"
runuser -u "$service_user" -- psql -d "$database" -tAc "SELECT 'PostgreSQL ' || current_setting('server_version') || ', conectado como ' || current_user"
echo "PostgreSQL escucha en: $(psql_postgres "SHOW listen_addresses")"
# sed y no head: head cerraría la tubería antes de tiempo y, con pipefail,
# el script acabaría con error.
ffprobe -version | sed -n 1p
flac --version
echo "Listo."
