# API de Bitabit

[`openapi.json`](openapi.json) es el contrato de la API propia, en OpenAPI 3.1. Lo usan las apps de Android y de escritorio. El servidor y los clientes lo obedecen: cualquier cambio de la API empieza por este archivo.

- **Versión 1.0.0**, bajo `/api/v1`. Lo compatible (campos, parámetros opcionales o endpoints nuevos) sube el segundo número; lo incompatible irá en `/api/v2`.
- Está en JSON para que los tests de Go y de Java puedan leerlo sin dependencias.

## Resumen

| Ruta | Para qué |
|---|---|
| `GET /api/v1/info` | Comprobar el servidor y su versión, sin sesión |
| `POST /api/v1/sessions` | Iniciar sesión con la contraseña de la cuenta |
| `GET /api/v1/sessions`, `DELETE /api/v1/sessions/{id}` | Ver y cerrar sesiones |
| `GET /api/v1/artists`, `/artists/{id}` | Artistas |
| `GET /api/v1/albums`, `/albums/{id}`, `/albums/{id}/cover` | Álbumes, sus pistas y su carátula |
| `GET /api/v1/tracks/{id}`, `/tracks/{id}/link` | Una pista y el enlace firmado para escucharla |
| `GET /api/v1/search` | Buscar, con tolerancia a erratas y tildes |
| `GET /media/{id}.{ext}` | El audio, con un enlace firmado |

## Validar

```bash
npx @redocly/cli@2.58.2 lint api/openapi.json
```

Da un aviso conocido, `operation-4xx-response` en `GET /api/v1/info`: ese endpoint no pide nada, así que no tiene respuestas 4xx. Cualquier otro aviso hay que mirarlo.
