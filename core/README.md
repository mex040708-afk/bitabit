# Módulo core

Librería en Java 17 que usan la app de Android y la de escritorio. Habla con el
servidor y no tiene interfaz. El contrato es `api/openapi.json` (ADR-014).

## Qué hay

- `BitabitClient`: el cliente de la API v1, sobre OkHttp. Inicia sesión, guarda el
  token y lo envía solo a los endpoints que lo piden. Sus llamadas esperan la
  respuesta, así que se hacen desde un hilo de fondo.
- `ApiException`: un error del servidor, con su estado HTTP, su código del contrato y,
  si los trae, los intentos que quedan o los segundos de bloqueo.
- `Contract`: los objetos de la API v1. Los campos que el contrato deja en `null` se
  leen como `Optional`, y las fechas, como `Instant`.
- `Json`: la lectura y la escritura con Gson. Ignora los campos que no conoce.
- `PlayQueue`: la cola de reproducción, con aleatorio que recuerda el orden original
  y repetición (`OFF`, `ALL`, `ONE`). Solo guarda el estado y avisa de los cambios;
  no es segura entre hilos (ADR-022).
- `QualityPolicy`: qué versión pedir, original o AAC de 256 kbit/s, según la red y la
  preferencia del usuario. Si la pista no tiene la elegida, usa la otra (ADR-022).

## Uso

```java
BitabitClient client = new BitabitClient("http://servidor:8080");
client.login("la contraseña", "Pixel 7a");

Contract.Link link = client.link(pista.getId(), BitabitClient.Quality.ORIGINAL);
String url = client.mediaUrl(link); // la dirección completa, para el reproductor
```

Un error del servidor llega como `ApiException`, con el estado HTTP y el código del
contrato:

```java
try {
    client.login(contraseña, "Pixel 7a");
} catch (ApiException e) {
    // e.status() == 401 y e.attemptsLeft() dicen cuántos intentos quedan;
    // e.status() == 429 y e.retryAfter() dicen cuántos segundos faltan.
}
```

## Qué falta

- Caché con ETag: el cliente pide cada respuesta entera, sin `If-None-Match`.
- Pruebas en un dispositivo Android: lo probado hasta ahora corre en la JVM.

## Compilar y probar

Con Gradle 9.5, el mismo que `desktop/`:

```bash
gradle test
```

El repositorio no versiona el `gradle-wrapper.jar`, por ser binario. Para usar
`./gradlew`, cópialo de `desktop/gradle/wrapper/`.

## Para Android

R8 necesita una regla para los modelos, porque Gson los lee por reflexión:

```
-keep class io.github.mex040708afk.bitabit.core.Contract$** { *; }
```

`Optional` (desde la API 24) e `Instant` (desde la 26) necesitan la desugaring de
librerías de Java en las versiones anteriores de Android.
