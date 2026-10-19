# Bitabit

Servicio de streaming de música casero para uso personal. Sirve una biblioteca propia y entrega cada canción **bit a bit**: el archivo original sin pérdida, sin remuestrear ni recodificar.

> **Estado:** primeras piezas del servidor. Todavía no se puede escuchar nada.

## Servidor

Necesita Go 1.27 o posterior.

```bash
cd server
go vet ./...
go test ./...
```

## Piezas previstas

| Pieza | Tecnología |
|---|---|
| Servidor | Go |
| Base de datos | PostgreSQL |
| App Android | Java + Media3 |
| App de escritorio | Java (JavaFX) + mpv |

## Licencia

[MIT](LICENSE)
