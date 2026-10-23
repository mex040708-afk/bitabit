# Bitabit para Android

La app entra con la contraseña del servidor (guarda solo el token, cifrado con el Android Keystore) y tiene una pantalla principal provisional. La pantalla «Diagnóstico» de la fase 1 reproduce un enlace firmado del servidor y, al terminar, enseña el MD5 de lo que ha sonado para compararlo con el del original.

## Compilar

Con Android Studio basta con abrir esta carpeta (`android/`).

Por consola hace falta el `gradle-wrapper.jar`, que no está en Git porque es un binario. Se genera una vez con Gradle 9.5.0:

```bash
gradle wrapper --gradle-version 9.5.0 --distribution-type bin \
  --gradle-distribution-sha256-sum 553c78f50dafcd54d65b9a444649057857469edf836431389695608536d6b746
```

Después, con un JDK 21 (por ejemplo, el que trae Android Studio):

```bash
./gradlew testDebugUnitTest assembleDebug
```

## Probar en el emulador

1. Arrancar el servidor. En el emulador, el servidor es `http://10.0.2.2:8080`, que es el `localhost` del ordenador.
2. Instalar: `adb install -r app/build/outputs/apk/debug/app-debug.apk`.
3. Entrar con la contraseña del servidor. «Cerrar sesión» cierra la sesión en el servidor y borra el token.
4. «Diagnóstico» abre la pantalla de la fase 1: pedir un enlace con `bitabit link <pista>`, pegarlo y comparar el MD5 que sale con el del original.

## Arrancar el diagnóstico desde adb

Sin pasar por el inicio de sesión ni pegar el enlace a mano:

```bash
adb shell am start -n io.github.mex040708afk.bitabit/.DiagnosticActivity   --es enlace "<enlace de bitabit link>" --es servidor http://10.0.2.2:8080 --ez reproducir true
```

`servidor` y `reproducir` son opcionales; con `reproducir` empieza a sonar sin pulsar el botón. En Git Bash de Windows, `MSYS_NO_PATHCONV=1` evita que cambie las rutas, y los `&` del enlace van escapados (`\&`).
