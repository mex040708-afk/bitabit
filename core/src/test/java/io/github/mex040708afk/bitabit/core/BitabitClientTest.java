package io.github.mex040708afk.bitabit.core;

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertThrows;
import static org.junit.Assert.assertTrue;

import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import io.github.mex040708afk.bitabit.core.BitabitClient.AlbumSort;
import io.github.mex040708afk.bitabit.core.BitabitClient.Quality;
import io.github.mex040708afk.bitabit.core.Contract.Link;
import io.github.mex040708afk.bitabit.core.Contract.Login;
import java.io.IOException;
import java.util.Optional;
import okhttp3.mockwebserver.MockResponse;
import okhttp3.mockwebserver.MockWebServer;
import okhttp3.mockwebserver.RecordedRequest;
import okio.Buffer;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;

/**
 * El cliente contra un servidor simulado: comprueba la ruta, la cabecera de sesión y el
 * cuerpo de cada petición, y cómo se leen las respuestas y los errores.
 */
public class BitabitClientTest {

    private static final String TOKEN = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8";
    private static final String LOGIN_BODY = "{\"token\":\"" + TOKEN + "\",\"session\":{\"id\":7,"
            + "\"device\":\"Pixel 7a\",\"createdAt\":\"2026-10-22T08:00:00Z\","
            + "\"lastUsedAt\":\"2026-10-22T08:00:00Z\",\"current\":true}}";

    private MockWebServer server;
    private BitabitClient client;

    @Before
    public void startServer() throws IOException {
        server = new MockWebServer();
        server.start();
        client = new BitabitClient(server.url("/").toString());
    }

    @After
    public void stopServer() throws IOException {
        server.shutdown();
    }

    @Test
    public void infoNeedsNoSession() throws Exception {
        server.enqueue(json(200, "{\"service\":\"bitabit\",\"apiVersion\":\"1.0.0\",\"serverVersion\":\"0.2.0\"}"));

        assertEquals("1.0.0", client.info().getApiVersion());

        RecordedRequest request = server.takeRequest();
        assertEquals("GET", request.getMethod());
        assertEquals("/api/v1/info", request.getPath());
        assertNull(request.getHeader("Authorization"));
    }

    @Test
    public void loginSendsPasswordAndTrimmedDeviceAndKeepsTheToken() throws Exception {
        server.enqueue(json(201, LOGIN_BODY));

        Login login = client.login("secreto", "  Pixel 7a  ");

        RecordedRequest request = server.takeRequest();
        assertEquals("POST", request.getMethod());
        assertEquals("/api/v1/sessions", request.getPath());
        JsonObject sent = JsonParser.parseString(request.getBody().readUtf8()).getAsJsonObject();
        assertEquals(2, sent.size());
        assertEquals("secreto", sent.get("password").getAsString());
        assertEquals("Pixel 7a", sent.get("device").getAsString());
        assertEquals(TOKEN, login.getToken());
        assertEquals(Optional.of(TOKEN), client.token());
    }

    @Test
    public void callsWithASessionSendTheBearerToken() throws Exception {
        signIn();
        server.enqueue(json(200, "{\"items\":[]}"));

        assertTrue(client.sessions().isEmpty());

        RecordedRequest request = server.takeRequest();
        assertEquals("GET", request.getMethod());
        assertEquals("/api/v1/sessions", request.getPath());
        assertEquals("Bearer " + TOKEN, request.getHeader("Authorization"));
    }

    @Test
    public void callsThatNeedASessionFailBeforeSendingWithoutOne() throws Exception {
        assertThrows(IllegalStateException.class, () -> client.artists());

        assertEquals(0, server.getRequestCount());
    }

    @Test
    public void wrongPasswordTellsHowManyAttemptsAreLeft() throws Exception {
        server.enqueue(json(401, "{\"error\":\"wrong_password\",\"message\":\"Contraseña equivocada.\","
                + "\"attemptsLeft\":8}"));

        ApiException error = assertThrows(ApiException.class, () -> client.login("mal", "Pixel 7a"));

        assertEquals(401, error.status());
        assertEquals("wrong_password", error.code());
        assertEquals(Optional.of(8), error.attemptsLeft());
        assertFalse(error.retryAfter().isPresent());
        assertFalse(client.token().isPresent());
    }

    @Test
    public void blockedTellsHowLongToWait() throws Exception {
        server.enqueue(json(429, "{\"error\":\"blocked\",\"message\":\"Demasiados fallos desde esta IP.\","
                + "\"retryAfter\":86400}"));

        ApiException error = assertThrows(ApiException.class, () -> client.login("mal", "Pixel 7a"));

        assertEquals(429, error.status());
        assertEquals("blocked", error.code());
        assertEquals(Optional.of(86400), error.retryAfter());
        assertFalse(error.attemptsLeft().isPresent());
    }

    @Test
    public void expiredTokenIsAnUnauthorizedError() throws Exception {
        signIn();
        server.enqueue(json(401, "{\"error\":\"unauthorized\",\"message\":\"Falta el token o ya no vale.\"}"));

        ApiException error = assertThrows(ApiException.class, () -> client.artists());

        assertEquals(401, error.status());
        assertEquals("unauthorized", error.code());
    }

    @Test
    public void anErrorWithoutAContractBodyStillHasItsStatus() throws Exception {
        server.enqueue(new MockResponse().setResponseCode(500).setBody("<h1>Error</h1>"));

        ApiException error = assertThrows(ApiException.class, () -> client.info());

        assertEquals(500, error.status());
        assertEquals("", error.code());
    }

    @Test
    public void aSuccessWithAnUnreadableBodyIsAnIOError() throws Exception {
        server.enqueue(json(200, "no es json"));

        IOException error = assertThrows(IOException.class, () -> client.info());

        assertFalse(error instanceof ApiException);
    }

    @Test
    public void invalidInputNeverReachesTheServer() throws Exception {
        assertThrows(IllegalArgumentException.class, () -> client.login("", "Pixel 7a"));
        assertThrows(IllegalArgumentException.class, () -> client.login("secreto", "   "));
        assertThrows(IllegalArgumentException.class, () -> client.search("   ", 10));
        assertThrows(IllegalArgumentException.class, () -> client.search("abc", 51));
        assertThrows(IllegalArgumentException.class, () -> client.album(0));
        assertThrows(IllegalArgumentException.class, () -> client.closeSession(0));
        assertThrows(IllegalArgumentException.class, () -> client.useToken("corto"));

        assertEquals(0, server.getRequestCount());
    }

    @Test
    public void theServerAddressHasNoPath() {
        assertThrows(IllegalArgumentException.class, () -> new BitabitClient("ftp://servidor:21"));
        assertThrows(IllegalArgumentException.class, () -> new BitabitClient("http://servidor:8080/bitabit"));
        assertThrows(IllegalArgumentException.class, () -> new BitabitClient("no es una dirección"));
    }

    @Test
    public void searchTrimsTheQueryAndSendsTheLimit() throws Exception {
        signIn();
        server.enqueue(json(200, "{\"artists\":[],\"albums\":[],\"tracks\":[]}"));

        client.search("  beatels  ", 5);

        assertEquals("/api/v1/search?q=beatels&limit=5", server.takeRequest().getPath());
    }

    @Test
    public void albumsAskForTheOrder() throws Exception {
        signIn();
        server.enqueue(json(200, "{\"items\":[]}"));

        assertTrue(client.albums(AlbumSort.ADDED).isEmpty());

        assertEquals("/api/v1/albums?sort=added", server.takeRequest().getPath());
    }

    @Test
    public void linkAsksForTheQualityAndMediaUrlIsItsFullAddress() throws Exception {
        signIn();
        server.enqueue(json(200, "{\"path\":\"/media/4312.flac?q=aac256&exp=1767225600&kid=k1&sig=abc\","
                + "\"quality\":\"aac256\",\"expiresAt\":\"2026-01-01T12:00:00Z\"}"));

        Link link = client.link(4312, Quality.AAC_256);

        assertEquals("/api/v1/tracks/4312/link?quality=aac256", server.takeRequest().getPath());
        assertEquals(server.url("/").toString() + "media/4312.flac?q=aac256&exp=1767225600&kid=k1&sig=abc",
                client.mediaUrl(link));
    }

    @Test
    public void closingASessionKeepsTheTokenInTheClient() throws Exception {
        signIn();
        server.enqueue(new MockResponse().setResponseCode(204));

        client.closeSession(3);

        RecordedRequest request = server.takeRequest();
        assertEquals("DELETE", request.getMethod());
        assertEquals("/api/v1/sessions/3", request.getPath());
        assertEquals("Bearer " + TOKEN, request.getHeader("Authorization"));
        assertEquals(Optional.of(TOKEN), client.token());
    }

    @Test
    public void forgettingTheTokenStopsTheCalls() throws Exception {
        signIn();

        client.forgetToken();

        assertFalse(client.token().isPresent());
        assertThrows(IllegalStateException.class, () -> client.artists());
    }

    @Test
    public void aSavedTokenRestoresTheSession() throws Exception {
        client.useToken(TOKEN);
        server.enqueue(json(200, "{\"items\":[]}"));

        assertTrue(client.artists().isEmpty());

        assertEquals("Bearer " + TOKEN, server.takeRequest().getHeader("Authorization"));
    }

    @Test
    public void catalogCallsUseTheirContractPaths() throws Exception {
        signIn();
        server.enqueue(json(200, "{\"items\":[]}"));
        server.enqueue(json(200, "{\"id\":3,\"name\":\"Grupo\",\"albums\":[]}"));
        server.enqueue(json(200, "{\"id\":4,\"title\":\"Disco\",\"artist\":{\"id\":3,\"name\":\"Grupo\"},"
                + "\"year\":null,\"trackCount\":1,\"durationMs\":60000,\"addedAt\":\"2026-10-21T10:00:00Z\","
                + "\"hasCover\":false,\"tracks\":[]}"));
        server.enqueue(json(200, "{\"id\":9,\"title\":\"Uno\",\"artist\":\"Grupo\","
                + "\"album\":{\"id\":4,\"title\":\"Disco\"},\"albumArtist\":{\"id\":3,\"name\":\"Grupo\"},"
                + "\"discNumber\":null,\"trackNumber\":1,\"durationMs\":120000,\"files\":[]}"));

        client.artists();
        client.artist(3);
        client.album(4);
        client.track(9);

        assertEquals("/api/v1/artists", server.takeRequest().getPath());
        assertEquals("/api/v1/artists/3", server.takeRequest().getPath());
        assertEquals("/api/v1/albums/4", server.takeRequest().getPath());
        assertEquals("/api/v1/tracks/9", server.takeRequest().getPath());
    }

    @Test
    public void coverIsTheImageAsTheServerSendsIt() throws Exception {
        signIn();
        byte[] jpeg = {(byte) 0xFF, (byte) 0xD8, (byte) 0xFF, (byte) 0xE0, 0x00, 0x10};
        server.enqueue(new MockResponse().setHeader("Content-Type", "image/jpeg")
                .setBody(new Buffer().write(jpeg)));

        byte[] cover = client.cover(4).orElseThrow();

        assertEquals("/api/v1/albums/4/cover", server.takeRequest().getPath());
        assertArrayEquals(jpeg, cover);
    }

    @Test
    public void anAlbumWithoutCoverHasNoImage() throws Exception {
        signIn();
        server.enqueue(json(404, "{\"error\":\"not_found\",\"message\":\"No existe.\"}"));

        assertFalse(client.cover(4).isPresent());
    }

    /** Abre una sesión y descarta su petición, para que la siguiente sea la de cada prueba. */
    private void signIn() throws Exception {
        server.enqueue(json(201, LOGIN_BODY));
        client.login("secreto", "Pixel 7a");
        server.takeRequest();
    }

    private static MockResponse json(int status, String body) {
        return new MockResponse().setResponseCode(status)
                .setHeader("Content-Type", "application/json")
                .setBody(body);
    }
}
