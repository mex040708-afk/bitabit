# Bitabit para Android

App mínima de la fase 1: reproduce un enlace firmado del servidor y, al terminar, enseña el MD5 de lo que ha sonado para compararlo con el del original.

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

1. Arrancar el servidor y pedir un enlace: `bitabit link <pista>`.
2. Instalar: `adb install -r app/build/outputs/apk/debug/app-debug.apk`.
3. Pegar el enlace en la app. En el emulador, el servidor es `http://10.0.2.2:8080`, que es el `localhost` del ordenador.
4. Escuchar la pista entera sin saltos y comparar el MD5 que sale con el del original.
