# Bitabit para escritorio

Prototipo de la fase 1, para Windows. JavaFX pinta la ventana y mpv reproduce el audio, controlado por su tubería JSON. Con un enlace firmado del servidor se puede:

- **Escuchar:** suena por la tarjeta. Enseña el formato que da el decodificador y el que mpv manda a Windows, y dice si son iguales. En modo exclusivo (WASAPI) tienen que serlo; en modo compartido Windows lo pasa todo a su formato de mezcla.
- **Medir:** mpv decodifica la pista sin sonar (`--ao=pcm`) y la app calcula el MD5 de lo que habría mandado a la tarjeta. Lo compara con el MD5 de STREAMINFO del original. Solo con FLAC.

## Requisitos

- mpv. Con winget: `winget install --id shinchiro.mpv --exact`. La app lo busca en la variable `BITABIT_MPV` y, si no está, en el `PATH`. Por ejemplo, en PowerShell:

  ```powershell
  $env:BITABIT_MPV = "C:\Program Files\MPV Player\mpv.exe"
  ```

- Un JDK 21, por ejemplo el que trae Android Studio.

## Compilar y arrancar

Por consola hace falta el `gradle-wrapper.jar`, que no está en Git porque es un binario. Se genera una vez con Gradle 9.5.0:

```bash
gradle wrapper --gradle-version 9.5.0 --distribution-type bin \
  --gradle-distribution-sha256-sum 553c78f50dafcd54d65b9a444649057857469edf836431389695608536d6b746
```

Después:

```bash
./gradlew build
./gradlew run
```

Con `BITABIT_MPV` puesta, `build` pasa también las pruebas que hablan con un mpv de verdad.

## Probar

1. Arrancar el servidor y pedir un enlace: `bitabit link <pista>`.
2. Pegarlo en la ventana y pulsar **Medir** o **Escuchar**.

Para las pruebas automáticas, el enlace se puede leer de un archivo, para que no salga en la lista de procesos:

```bash
./gradlew run --args="--enlace-archivo=enlace.txt --accion=medir --salir"
```

`--accion` puede ser `medir` o `escuchar`, y `--exclusivo=no` escucha en modo compartido. Con `--salir`, la app se cierra al terminar con el código 0 si todo cuadra y 1 si no. El resultado sale también por la salida estándar, sin la firma del enlace.
