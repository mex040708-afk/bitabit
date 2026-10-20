# Bitabit

Servicio de streaming de música casero para uso personal. Sirve una biblioteca propia y entrega cada canción **bit a bit**: el archivo original sin pérdida, sin remuestrear ni recodificar.

> **Estado:** prueba de la cadena de audio. El servidor importa y sirve canciones, y hay una app Android y un prototipo de escritorio que comprueban que lo que suena es el original bit a bit.

## Servidor

Necesita Go 1.27 o posterior.

```bash
cd server
go vet ./...
go test ./...
```

## Clientes

- [Android](android/README.md)
- [Escritorio](desktop/README.md), para Windows

## Piezas previstas

| Pieza | Tecnología |
|---|---|
| Servidor | Go |
| Base de datos | PostgreSQL |
| App Android | Java + Media3 |
| App de escritorio | Java (JavaFX) + mpv |

## Licencia

[MIT](LICENSE)
