# Bitabit para escritorio

App de Windows. JavaFX pinta la ventana, el módulo `core` (`../core`) habla con la API v1 del servidor y mpv reproduce el audio, controlado por su tubería JSON.

## Sesión

Al arrancar pide la dirección del servidor (`http://host:puerto`) y la contraseña de la cuenta. El equipo sale en la lista de sesiones con su nombre (`COMPUTERNAME`), o como «Escritorio».

La contraseña no se guarda nunca. Se guardan solo el servidor, el id de la sesión y su token, en `%APPDATA%\Bitabit\session.json` (ADR-025). Al arrancar otra vez, la app comprueba la sesión con el servidor:

- si ya no vale, borra el archivo y pide la contraseña;
- si el servidor no responde, lo dice y ofrece reintentar, sin borrar nada;
- si el archivo está mal formado, lo borra y explica por qué.

**Cerrar sesión** la cierra en el servidor y borra el archivo; si el servidor no responde, el archivo se borra igual y la app avisa.

## Catálogo

Con la sesión abierta, la ventana tiene navegación lateral (ADR-026): **Artistas**, **Álbumes** y **Diagnóstico** (el panel del prototipo). Un artista abre sus álbumes en una rejilla con carátulas y un álbum abre su tabla de pistas (número, título, duración `m:ss` o `h:mm:ss`, y formato como `FLAC 24/96` o `MP3 44.1 kHz`). «Atrás» vuelve sin recargar. Los nombres vacíos se pintan como «(sin nombre)» en cursiva y los álbumes sin carátula (o con una que falla) enseñan un marcador.

- Las listas son `ListView`/`TableView`, que solo crean nodos para lo que se ve; la rejilla es un `ListView` de filas de álbumes.
- Las carátulas se piden en un grupo de hilos aparte, se reducen a 240 px al decodificarlas y se guardan en una caché LRU de 200. Un fallo se apunta en el registro (`System.Logger`) y deja el marcador.
- Si el servidor responde 401 en cualquier llamada, se borra la sesión guardada y se vuelve al inicio de sesión.
- Core todavía no pide `?size=256` (API 1.1.0) ni usa `ETag`: se descarga la carátula original.

## Diagnóstico

Debajo de la sesión sigue el panel del prototipo de la fase 1. Con un enlace firmado del servidor se puede:

- **Escuchar:** suena por la tarjeta. Enseña el formato que da el decodificador y el que mpv manda a Windows, y dice si son iguales. En modo exclusivo (WASAPI) tienen que serlo; en modo compartido Windows lo pasa todo a su formato de mezcla.
- **Medir:** mpv decodifica la pista sin sonar (`--ao=pcm`) y la app calcula el MD5 de lo que habría mandado a la tarjeta. Lo compara con el MD5 de STREAMINFO del original. Solo con FLAC.

## Código

Todo está en el paquete `io.github.mex040708afk.bitabit.desktop`:

- `Mpv`: un proceso de mpv y su tubería JSON. Las opciones que tocan el audio son siempre las mismas (volumen a 100, sin ReplayGain).
- `Player`: el reproductor de la app (ADR-020). Un único mpv vivo con `--idle=yes`; `play`, `pause`, `resume`, `togglePause`, `seek`, `stop` y el modo exclusivo, que se cambia sin reiniciar mpv. Avisa a un `Player.Listener` de la posición, la duración, la pausa, los formatos y el fin de cada pista con su motivo (`EndReason`). No conoce la cola. `PlayerEvents` traduce los mensajes de mpv a esos avisos.
- `Listen` y `Measure`: las dos acciones del prototipo. `App` es la ventana.

Los avisos de mpv llegan desde el hilo que lee la tubería: la interfaz los pasa a JavaFX con `Platform.runLater` y no puede mandar comandos desde ese hilo.

## Requisitos

- mpv. Con winget: `winget install --id shinchiro.mpv --exact`. La app lo busca en la variable `BITABIT_MPV` y, si no está, en el `PATH`. Por ejemplo, en PowerShell:

  ```powershell
  $env:BITABIT_MPV = "C:\Program Files\MPV Player\mpv.exe"
  ```

- Un JDK 21, por ejemplo el que trae Android Studio.
- El directorio `core/` al lado de `desktop/`, como en el repositorio: el build lo incluye con `includeBuild("../core")` (ADR-020) y lo compila solo.

## Compilar y arrancar

Por consola hace falta el `gradle-wrapper.jar`, que no está en Git porque es un binario. Se genera una vez con Gradle 9.5.0:

```bash
gradle wrapper --gradle-version 9.5.0 --distribution-type bin \
  --gradle-distribution-sha256-sum 553c78f50dafcd54d65b9a444649057857469edf836431389695608536d6b746
```

Después (`core` usa el mismo Gradle 9.5, y desde `desktop/` no le hace falta su propio wrapper):

```bash
./gradlew build
./gradlew run
```

Con `BITABIT_MPV` puesta, `build` pasa también las pruebas que hablan con un mpv de verdad. Las del `Player` necesitan además FFmpeg en el `PATH`: generan un tono en una carpeta temporal y lo reproducen con `--ao=null`, sin que suene la tarjeta. Las de la sesión y las del catálogo usan un servidor simulado (`com.sun.net.httpserver`, del JDK) y una carpeta temporal en lugar de `%APPDATA%`.

OkHttp y Okio, que trae `core`, son módulos automáticos y no declaran que necesitan `kotlin.stdlib`; por eso la app arranca con `--add-modules=kotlin.stdlib` (está en `build.gradle.kts`, en `run` y en los scripts de la distribución).

## Probar

1. Arrancar el servidor y pedir un enlace: `bitabit link <pista>`.
2. Iniciar sesión, pegarlo en el panel de diagnóstico y pulsar **Medir** o **Escuchar**.

Para las pruebas automáticas, sin iniciar sesión, el enlace se puede leer de un archivo, para que no salga en la lista de procesos:

```bash
./gradlew run --args="--enlace-archivo=enlace.txt --accion=medir --salir"
```

Con `--enlace-archivo` o `--accion`, la app abre directamente el panel de diagnóstico, sin pedir sesión. `--accion` puede ser `medir` o `escuchar`, y `--exclusivo=no` escucha en modo compartido. Con `--salir`, la app se cierra al terminar con el código 0 si todo cuadra y 1 si no. El resultado sale también por la salida estándar, sin la firma del enlace.
