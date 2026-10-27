# Reglas de R8 de la app. Hoy la optimización está desactivada en release, pero el
# build ya las enlaza para que no se olviden el día que se active.

# core lee los modelos del contrato con Gson, por reflexión (core/README.md).
-keep class io.github.mex040708afk.bitabit.core.Contract$** { *; }
