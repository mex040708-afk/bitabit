# Capa OpenSubsonic (`/rest/`)

Contrato del subconjunto de [OpenSubsonic](https://opensubsonic.netlify.app/) que implementa el servidor, para clientes Subsonic existentes (el del iPhone). No sustituye a `openapi.json`, que es la API propia de las apps de Android y escritorio. Mientras no haya un cliente que lo pida, solo se implementa lo que está en la tabla.

## Reglas generales

- Se anuncia Subsonic API **1.16.1** con `openSubsonic: true`, `type: "bitabit"` y la versión del servidor en `serverVersion`.
- Cada función responde en `/rest/{función}` y en `/rest/{función}.view`, con GET o POST de formulario (cuerpo de hasta 16 KiB).
- Formato: `f=json` da `{"subsonic-response": {...}}`; sin `f` (o con `f=xml`) da XML con el espacio de nombres `http://subsonic.org/restapi`.
- Las respuestas, también las de error, salen con **HTTP 200**; el fallo va en `status: "failed"` y en `error {code, message}`.
- Los ids van como cadenas y las duraciones en segundos, como pide la especificación. Una función que no está en la tabla responde el error `0` («Función no implementada»).
- Los parámetros llevan las credenciales: el servidor no los escribe en sus registros.

## Autenticación (ADR-031)

Cuenta única: el valor de `u` se ignora.

| Mecanismo | Parámetros | Resultado |
|---|---|---|
| Clave | `apiKey` = token de una sesión de `POST /api/v1/sessions` | Vale mientras la sesión exista. Sin `u`. |
| Contraseña | `u` y `p` (en claro o `enc:` + hexadecimal) | Se comprueba con el hash guardado y cuenta para el bloqueo por IP (10 fallos, 24 h). No abre sesión. |
| Token con salt | `u`, `t`, `s` | **No admitido**: error `42`. |

Errores: `10` faltan credenciales · `40` usuario o contraseña incorrectos, o IP bloqueada · `42` mecanismo no admitido · `43` más de un mecanismo, o `apiKey` con `u` · `44` `apiKey` no válida.

Usa siempre HTTPS fuera de casa: con `u` y `p` la contraseña viaja en cada petición.

## Funciones

| Función | Autenticación | Tanda | Estado |
|---|---|---|---|
| `ping` | sí | 1 | hecha |
| `getLicense` | sí | 1 | hecha: `license {valid: true}` |
| `getOpenSubsonicExtensions` | no | 1 | hecha: solo `apiKeyAuthentication` v1 |
| `getMusicFolders` | sí | 2 | hecha: una carpeta, «Biblioteca» (id 1) |
| `getArtists` | sí | 2 | hecha: agrupados por inicial, `#` para lo que no empieza por letra; `ignoredArticles` vacío |
| `getArtist` | sí | 2 | hecha: con sus álbumes; error 10 sin `id`, error 70 si no existe |
| `getAlbum` | sí | 2 | hecha: álbum con sus pistas (la versión original, o la única que haya) |
| `getSong` | sí | 2 | hecha |
| `getAlbumList2` | sí | 2 | hecha: `newest` por fecha de importación; el resto, por nombre. `size` (10, máx. 500) y `offset`. Sin historial ni favoritos, `random`, `frequent`, `recent`, `highest`, `starred` y `byYear` caen en el orden por nombre |
| `search3` | sí | 2 | hecha: artistas, álbumes y pistas con `artistCount`/`albumCount`/`songCount` y sus `…Offset`. Una búsqueda vacía (`""`) lista artistas y álbumes, sin pistas |
| `getCoverArt` | sí | 2 | hecha: `id` es el del álbum (el campo `coverArt`); con `size` ≤ 256 sirve la miniatura si existe |
| `stream` | sí | 3 | hecha: responde **302** a un enlace firmado de `/media` (el audio lo sirve `/media`, con `Range`). Sin `maxBitRate` o con `format=raw`, el original; con `maxBitRate` por debajo de la tasa del original y AAC 256 disponible, el AAC. Nada se convierte al vuelo |
| `download` | sí | 3 | hecha: igual, siempre el original |
| `scrobble` | sí | 3 | hecha: no guarda nada (no hay historial) |
| `getUser` | sí | 4 | hecha: la cuenta única puede escuchar, descargar y pedir carátulas; ningún otro permiso |
| `getPlaylists`, `getStarred2`, `getGenres` | sí | 4 | hechas: siempre vacías (no hay listas, favoritos ni géneros) |
| `getScanStatus` | sí | 4 | hecha: nunca hay un escaneo en curso (importa `entrada/`) |

La tanda 4 son funciones que muchos clientes llaman al arrancar y se niegan a seguir si fallan; no se han probado con un cliente concreto.
