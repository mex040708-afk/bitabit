# Módulo core

Librería en Java 17 que usan la app de Android y la de escritorio. Contiene lo que
hablan con el servidor, sin interfaz. El contrato es `api/openapi.json` (ADR-014).

## Qué hay

- `Contract`: los objetos de la API v1 (información, sesiones, artistas, álbumes,
  pistas, versiones, enlaces, búsqueda y errores). Los campos que el contrato deja
  en `null` se leen como `Optional`, y las fechas, como `Instant`.
- `Json`: la lectura de los cuerpos con Gson. Ignora los campos que no conoce.

## Qué falta

- El cliente HTTP con OkHttp. Necesita descargar OkHttp, que todavía no está en el
  equipo.

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
