package io.github.mex040708afk.bitabit.core;

import com.google.gson.JsonParseException;
import io.github.mex040708afk.bitabit.core.Contract.Album;
import io.github.mex040708afk.bitabit.core.Contract.AlbumList;
import io.github.mex040708afk.bitabit.core.Contract.AlbumSummary;
import io.github.mex040708afk.bitabit.core.Contract.Artist;
import io.github.mex040708afk.bitabit.core.Contract.ArtistList;
import io.github.mex040708afk.bitabit.core.Contract.ArtistSummary;
import io.github.mex040708afk.bitabit.core.Contract.Info;
import io.github.mex040708afk.bitabit.core.Contract.Link;
import io.github.mex040708afk.bitabit.core.Contract.Login;
import io.github.mex040708afk.bitabit.core.Contract.LoginRequest;
import io.github.mex040708afk.bitabit.core.Contract.SearchResult;
import io.github.mex040708afk.bitabit.core.Contract.Session;
import io.github.mex040708afk.bitabit.core.Contract.SessionList;
import io.github.mex040708afk.bitabit.core.Contract.Track;
import java.io.IOException;
import java.util.List;
import java.util.Optional;
import java.util.concurrent.TimeUnit;
import java.util.regex.Pattern;
import okhttp3.HttpUrl;
import okhttp3.MediaType;
import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.RequestBody;
import okhttp3.Response;
import okhttp3.ResponseBody;

/**
 * Cliente síncrono de la API v1 ({@code api/openapi.json}). Cada llamada espera la
 * respuesta, así que las apps la hacen desde un hilo de fondo, nunca desde el de la
 * interfaz.
 *
 * <p>{@link #login} guarda el token de la sesión, y las llamadas que piden sesión lo
 * envían solas. Si el servidor ya no lo acepta, la llamada falla con
 * {@link ApiException} 401: hay que volver a iniciar sesión.
 *
 * <p>Los valores que el contrato no admite (una contraseña vacía, un límite de 51) se
 * rechazan antes de salir a la red, con {@link IllegalArgumentException}.
 */
public final class BitabitClient {

    private static final MediaType JSON = MediaType.get("application/json; charset=utf-8");
    private static final Pattern TOKEN_FORMAT = Pattern.compile("[A-Za-z0-9_-]{43}");
    private static final int MAX_QUERY_CHARS = 100;
    private static final int MAX_LIMIT = 50;

    private final HttpUrl baseUrl;
    private final OkHttpClient http;
    private volatile String token;

    /**
     * @param serverUrl dirección del servidor con su esquema, host y puerto, por ejemplo
     *        {@code http://servidor:8080}
     * @throws IllegalArgumentException si no es una dirección http o https de solo servidor
     */
    public BitabitClient(String serverUrl) {
        HttpUrl parsed = HttpUrl.parse(serverUrl);
        // /media se resuelve contra la raíz: una ruta delante del servidor se perdería al
        // construir la dirección de un enlace.
        if (parsed == null || !"/".equals(parsed.encodedPath()) || parsed.encodedQuery() != null) {
            throw new IllegalArgumentException("dirección de servidor no válida: " + serverUrl);
        }
        this.baseUrl = parsed;
        this.http = new OkHttpClient.Builder()
                .connectTimeout(10, TimeUnit.SECONDS)
                .readTimeout(30, TimeUnit.SECONDS)
                .build();
    }

    /** Calidad de una versión de la pista: la original o la AAC de 256 kbit/s. */
    public enum Quality {
        ORIGINAL("orig"),
        AAC_256("aac256");

        private final String wire;

        Quality(String wire) {
            this.wire = wire;
        }

        String wire() {
            return wire;
        }
    }

    /** Orden de la lista de álbumes (parámetro {@code sort} del contrato). */
    public enum AlbumSort {
        /** Por título y después por artista, sin distinguir mayúsculas ni tildes. */
        NAME("name"),
        /** Lo último importado, primero. */
        ADDED("added");

        private final String wire;

        AlbumSort(String wire) {
            this.wire = wire;
        }

        String wire() {
            return wire;
        }
    }

    /**
     * Datos del servidor, sin sesión. Sirve para comprobar que la dirección es de Bitabit:
     * las apps miran que el primer número de {@code apiVersion} sea 1, el de este cliente.
     */
    public Info info() throws IOException {
        return send(new Request.Builder().url(url("api/v1/info")).get().build(), Info.class);
    }

    /**
     * Inicia sesión con la contraseña de la cuenta y guarda el token. El servidor no lo
     * vuelve a mostrar: si se pierde, hay que iniciar sesión otra vez.
     *
     * @param device nombre para reconocer el dispositivo en la lista de sesiones; se
     *        quitan los espacios de los extremos y tiene que quedar de 1 a 64 caracteres
     * @throws ApiException 401 con {@link ApiException#attemptsLeft()} si la contraseña no
     *         vale; 429 con {@link ApiException#retryAfter()} si la IP está bloqueada
     */
    public Login login(String password, String device) throws IOException {
        requireLength("la contraseña", password, 1, 256);
        String name = device.trim();
        requireLength("el dispositivo", name, 1, 64);
        Request request = new Request.Builder()
                .url(url("api/v1/sessions"))
                .post(RequestBody.create(Json.toJson(new LoginRequest(password, name)), JSON))
                .build();
        Login login = send(request, Login.class);
        token = login.getToken();
        return login;
    }

    /** Los dispositivos con sesión abierta, los usados más recientemente primero. */
    public List<Session> sessions() throws IOException {
        return send(authorized(url("api/v1/sessions")).get().build(), SessionList.class).getItems();
    }

    /**
     * Cierra una sesión. Su token deja de valer en el servidor: si es el de este cliente,
     * la siguiente llamada responde 401 y hay que llamar a {@link #forgetToken()}.
     */
    public void closeSession(long sessionId) throws IOException {
        requirePositive("de la sesión", sessionId);
        execute(authorized(url("api/v1/sessions/" + sessionId)).delete().build());
    }

    /** Los artistas de álbum de toda la biblioteca, por nombre sin distinguir mayúsculas ni tildes. */
    public List<ArtistSummary> artists() throws IOException {
        return send(authorized(url("api/v1/artists")).get().build(), ArtistList.class).getItems();
    }

    /** Un artista con sus álbumes. */
    public Artist artist(long artistId) throws IOException {
        requirePositive("del artista", artistId);
        return send(authorized(url("api/v1/artists/" + artistId)).get().build(), Artist.class);
    }

    /** Todos los álbumes, en el orden pedido. */
    public List<AlbumSummary> albums(AlbumSort sort) throws IOException {
        HttpUrl url = url("api/v1/albums").newBuilder().addQueryParameter("sort", sort.wire()).build();
        return send(authorized(url).get().build(), AlbumList.class).getItems();
    }

    /** Un álbum con sus pistas, por disco y número. */
    public Album album(long albumId) throws IOException {
        requirePositive("del álbum", albumId);
        return send(authorized(url("api/v1/albums/" + albumId)).get().build(), Album.class);
    }

    /**
     * La carátula del álbum, tal como la guarda el servidor (JPEG o PNG). Vacío si el álbum
     * no existe o no tiene carátula: el contrato responde 404 en los dos casos.
     */
    public Optional<byte[]> cover(long albumId) throws IOException {
        requirePositive("del álbum", albumId);
        Request request = authorized(url("api/v1/albums/" + albumId + "/cover")).get().build();
        try (Response response = http.newCall(request).execute()) {
            if (response.code() == 404) {
                return Optional.empty();
            }
            ResponseBody body = response.body();
            if (!response.isSuccessful()) {
                throw ApiException.fromResponse(response.code(), text(body));
            }
            if (body == null) {
                throw new IOException("la carátula llegó sin cuerpo");
            }
            return Optional.of(body.bytes());
        }
    }

    /** Una pista con sus versiones. */
    public Track track(long trackId) throws IOException {
        requirePositive("de la pista", trackId);
        return send(authorized(url("api/v1/tracks/" + trackId)).get().build(), Track.class);
    }

    /**
     * Un enlace firmado para escuchar una versión de la pista. Caduca a las 12 horas: si el
     * reproductor recibe un 403, hay que pedir otro. Una versión que la pista no tiene
     * responde 404.
     */
    public Link link(long trackId, Quality quality) throws IOException {
        requirePositive("de la pista", trackId);
        HttpUrl url = url("api/v1/tracks/" + trackId + "/link").newBuilder()
                .addQueryParameter("quality", quality.wire())
                .build();
        return send(authorized(url).get().build(), Link.class);
    }

    /**
     * La dirección completa de un enlace, para dársela al reproductor. No lleva token: el
     * enlace ya va firmado.
     */
    public String mediaUrl(Link link) {
        HttpUrl resolved = baseUrl.resolve(link.getPath());
        if (resolved == null) {
            throw new IllegalArgumentException("ruta de enlace no válida: " + link.getPath());
        }
        return resolved.toString();
    }

    /**
     * Busca artistas, álbumes y pistas, de más a menos parecidos. Tolera erratas y tildes.
     *
     * @param query lo que se busca; se quitan los espacios de los extremos y tiene que
     *        quedar de 1 a 100 caracteres
     * @param limit resultados como mucho de cada tipo, de 1 a 50
     */
    public SearchResult search(String query, int limit) throws IOException {
        String trimmed = query.trim();
        requireLength("la búsqueda", trimmed, 1, MAX_QUERY_CHARS);
        if (limit < 1 || limit > MAX_LIMIT) {
            throw new IllegalArgumentException("el límite tiene que ser de 1 a " + MAX_LIMIT + ", y es " + limit);
        }
        HttpUrl url = url("api/v1/search").newBuilder()
                .addQueryParameter("q", trimmed)
                .addQueryParameter("limit", String.valueOf(limit))
                .build();
        return send(authorized(url).get().build(), SearchResult.class);
    }

    /** Olvida el token guardado. No cierra la sesión en el servidor: para eso está {@link #closeSession}. */
    public void forgetToken() {
        token = null;
    }

    /** El token de la sesión abierta, si la hay. Sirve para guardarlo entre arranques de la app. */
    public Optional<String> token() {
        return Optional.ofNullable(token);
    }

    /**
     * Retoma una sesión guardada, sin pedir la contraseña. Si el servidor ya no acepta el
     * token, las llamadas responden 401.
     *
     * @throws IllegalArgumentException si el token no tiene el formato del contrato
     */
    public void useToken(String savedToken) {
        if (!TOKEN_FORMAT.matcher(savedToken).matches()) {
            throw new IllegalArgumentException("el token no tiene el formato del contrato");
        }
        token = savedToken;
    }

    private HttpUrl url(String path) {
        return baseUrl.newBuilder().addPathSegments(path).build();
    }

    /** Una petición con el token de la sesión. Sin sesión abierta, no sale de aquí. */
    private Request.Builder authorized(HttpUrl url) {
        String current = token;
        if (current == null) {
            throw new IllegalStateException("no hay sesión abierta: hay que llamar antes a login() o useToken()");
        }
        return new Request.Builder().url(url).header("Authorization", "Bearer " + current);
    }

    /** Hace la petición y lee el cuerpo como {@code type}. */
    private <T> T send(Request request, Class<T> type) throws IOException {
        String body = execute(request);
        try {
            return Json.fromJson(body, type);
        } catch (JsonParseException | IllegalArgumentException e) {
            throw new IOException("respuesta ilegible de " + request.url(), e);
        }
    }

    /** Hace la petición y devuelve el cuerpo. Un error del servidor se lanza como {@link ApiException}. */
    private String execute(Request request) throws IOException {
        try (Response response = http.newCall(request).execute()) {
            String body = text(response.body());
            if (!response.isSuccessful()) {
                throw ApiException.fromResponse(response.code(), body);
            }
            return body;
        }
    }

    private static String text(ResponseBody body) throws IOException {
        return body == null ? "" : body.string();
    }

    private static void requirePositive(String what, long id) {
        if (id < 1) {
            throw new IllegalArgumentException("el id " + what + " tiene que ser 1 o más: " + id);
        }
    }

    private static void requireLength(String what, String value, int min, int max) {
        int length = value.codePointCount(0, value.length());
        if (length < min || length > max) {
            throw new IllegalArgumentException(what + " tiene que tener de " + min + " a " + max
                    + " caracteres, y tiene " + length);
        }
    }
}
