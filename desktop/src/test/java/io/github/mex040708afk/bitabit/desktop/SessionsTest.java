package io.github.mex040708afk.bitabit.desktop;

import static io.github.mex040708afk.bitabit.desktop.SessionStoreTest.TOKEN;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import io.github.mex040708afk.bitabit.core.ApiException;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import org.junit.After;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.TemporaryFolder;

/** Las sesiones contra un servidor simulado que responde como dice api/openapi.json. */
public class SessionsTest {

    /** Inventada: ningún servidor de verdad la tiene. */
    private static final String PASSWORD = "contraseña-de-prueba";
    private static final String DEVICE = "EQUIPO-PRUEBA";

    private static final String LOGIN_OK = "{\"token\":\"" + TOKEN + "\",\"session\":{\"id\":42,\"device\":\""
            + DEVICE + "\",\"createdAt\":\"2026-10-22T10:00:00Z\",\"lastUsedAt\":\"2026-10-22T10:00:00Z\","
            + "\"current\":true}}";
    private static final String SESSIONS_OK = "{\"items\":[{\"id\":42,\"device\":\"" + DEVICE
            + "\",\"createdAt\":\"2026-10-22T10:00:00Z\",\"lastUsedAt\":\"2026-10-22T10:00:00Z\",\"current\":true}]}";
    private static final String UNAUTHORIZED = "{\"error\":\"unauthorized\",\"message\":\"token no válido\"}";

    @Rule
    public final TemporaryFolder temp = new TemporaryFolder();

    private FakeServer server;
    private SessionStore store;
    private Sessions sessions;

    @Before
    public void setUp() throws IOException {
        server = new FakeServer();
        store = new SessionStore(temp.getRoot().toPath().resolve("Bitabit"));
        sessions = new Sessions(store, DEVICE);
    }

    @After
    public void tearDown() {
        server.close();
    }

    @Test
    public void loginSavesTheSession() throws IOException {
        server.reply("POST /api/v1/sessions", FakeServer.Reply.json(201, LOGIN_OK));
        Sessions.Active active = sessions.login(server.address() + "/", PASSWORD);

        assertEquals(new SavedSession(server.address(), 42, TOKEN), active.saved());
        assertEquals(new SessionStore.Found(active.saved()), store.load());
        FakeServer.Received login = server.received().get(0);
        assertEquals(Optional.empty(), login.authorization());
        assertTrue(login.body(), login.body().contains("\"device\":\"" + DEVICE + "\""));
        assertFalse(active.toString(), active.toString().contains(TOKEN));
    }

    @Test
    public void wrongPasswordSaysHowManyAttemptsAreLeft() throws IOException {
        server.reply("POST /api/v1/sessions", FakeServer.Reply.json(401,
                "{\"error\":\"wrong_password\",\"message\":\"contraseña equivocada\",\"attemptsLeft\":3}"));
        try {
            sessions.login(server.address(), PASSWORD);
            fail("la contraseña no valía");
        } catch (ApiException e) {
            assertEquals("Contraseña incorrecta. Quedan 3 intentos antes de que el servidor bloquee este equipo.",
                    Messages.login(server.address(), e));
        }
        assertEquals(new SessionStore.Missing(), store.load());
    }

    @Test
    public void blockedSaysHowLongToWait() throws IOException {
        server.reply("POST /api/v1/sessions", new FakeServer.Reply(429,
                "{\"error\":\"blocked\",\"message\":\"bloqueado\",\"retryAfter\":86340}", Map.of("Retry-After", "86340")));
        try {
            sessions.login(server.address(), PASSWORD);
            fail("la IP estaba bloqueada");
        } catch (ApiException e) {
            assertEquals("Demasiados intentos fallidos: el servidor ha bloqueado este equipo. "
                    + "Vuelve a probar dentro de 23 h 59 min.", Messages.login(server.address(), e));
        }
        assertEquals(new SessionStore.Missing(), store.load());
    }

    @Test
    public void anInvalidAddressOrAnEmptyPasswordNeverReachTheNetwork() throws IOException {
        for (String[] bad : new String[][] {{"servidor:8080", PASSWORD}, {server.address() + "/api", PASSWORD},
                {server.address(), ""}}) {
            try {
                sessions.login(bad[0], bad[1]);
                fail("debería rechazar " + bad[0]);
            } catch (IllegalArgumentException e) {
                assertFalse(e.getMessage().isBlank());
            }
        }
        assertEquals(List.of(), server.received());
    }

    @Test
    public void ifTheSessionCannotBeSavedItIsClosedOnTheServer() throws IOException {
        server.reply("POST /api/v1/sessions", FakeServer.Reply.json(201, LOGIN_OK));
        server.reply("DELETE /api/v1/sessions/42", FakeServer.Reply.json(204, ""));
        // Un archivo donde iría la carpeta: no se puede crear.
        Path notADir = temp.newFile("no-es-carpeta").toPath();
        Sessions unsaveable = new Sessions(new SessionStore(notADir), DEVICE);
        try {
            unsaveable.login(server.address(), PASSWORD);
            fail("no se podía guardar");
        } catch (IOException e) {
            assertFalse(e.getMessage(), e.getMessage().contains(TOKEN));
        }
        FakeServer.Received close = server.received().get(1);
        assertEquals("DELETE", close.method());
        assertEquals(Optional.of("Bearer " + TOKEN), close.authorization());
    }

    @Test
    public void nothingSavedAsksForThePassword() throws IOException {
        assertEquals(new Sessions.NeedsLogin(Optional.empty(), Optional.empty()), sessions.resume());
        assertEquals(List.of(), server.received());
    }

    @Test
    public void aSavedSessionIsCheckedWithTheServer() throws IOException {
        store.save(new SavedSession(server.address(), 42, TOKEN));
        server.reply("GET /api/v1/sessions", FakeServer.Reply.json(200, SESSIONS_OK));

        Sessions.Resume resume = sessions.resume();
        assertTrue(resume.toString(), resume instanceof Sessions.Resumed);
        assertEquals(Optional.of("Bearer " + TOKEN), server.received().get(0).authorization());
        assertFalse(resume.toString(), resume.toString().contains(TOKEN));
    }

    @Test
    public void aSavedSessionThatNoLongerWorksIsDeleted() throws IOException {
        store.save(new SavedSession(server.address(), 42, TOKEN));
        server.reply("GET /api/v1/sessions", FakeServer.Reply.json(401, UNAUTHORIZED));

        Sessions.Resume resume = sessions.resume();
        Sessions.NeedsLogin needsLogin = (Sessions.NeedsLogin) resume;
        assertEquals(Optional.of(server.address()), needsLogin.server());
        assertTrue(needsLogin.notice().orElseThrow().contains("ya no vale"));
        assertEquals(new SessionStore.Missing(), store.load());
    }

    @Test
    public void anUnreachableServerKeepsTheSession() throws IOException {
        String gone = server.address();
        server.close();
        SavedSession saved = new SavedSession(gone, 42, TOKEN);
        store.save(saved);

        Sessions.Resume resume = sessions.resume();
        Sessions.Unreachable unreachable = (Sessions.Unreachable) resume;
        assertTrue(unreachable.message(), unreachable.message().contains(gone));
        assertFalse(unreachable.message(), unreachable.message().contains(TOKEN));
        assertEquals(new SessionStore.Found(saved), store.load());
    }

    @Test
    public void aServerErrorKeepsTheSession() throws IOException {
        SavedSession saved = new SavedSession(server.address(), 42, TOKEN);
        store.save(saved);
        server.reply("GET /api/v1/sessions", FakeServer.Reply.json(500, "{\"error\":\"internal\",\"message\":\"x\"}"));

        assertTrue(sessions.resume() instanceof Sessions.Unreachable);
        assertEquals(new SessionStore.Found(saved), store.load());
    }

    @Test
    public void aTokenWithTheWrongFormatIsDeletedWithoutAsking() throws IOException {
        store.save(new SavedSession(server.address(), 42, "corto"));

        Sessions.NeedsLogin needsLogin = (Sessions.NeedsLogin) sessions.resume();
        assertTrue(needsLogin.notice().orElseThrow().contains("mal formada"));
        assertEquals(new SessionStore.Missing(), store.load());
        assertEquals(List.of(), server.received());
    }

    @Test
    public void aMalformedFileAsksForThePasswordAndSaysWhy() throws IOException {
        Files.createDirectories(store.file().getParent());
        Files.writeString(store.file(), "{roto " + TOKEN);

        Sessions.NeedsLogin needsLogin = (Sessions.NeedsLogin) sessions.resume();
        String notice = needsLogin.notice().orElseThrow();
        assertTrue(notice, notice.contains("mal formado"));
        assertFalse(notice, notice.contains(TOKEN));
        assertFalse(Files.exists(store.file()));
    }

    @Test
    public void logoutClosesTheSessionAndDeletesTheFile() throws IOException {
        server.reply("POST /api/v1/sessions", FakeServer.Reply.json(201, LOGIN_OK));
        server.reply("DELETE /api/v1/sessions/42", FakeServer.Reply.json(204, ""));
        Sessions.Active active = sessions.login(server.address(), PASSWORD);

        assertEquals(Optional.empty(), sessions.logout(active));
        FakeServer.Received close = server.received().get(1);
        assertEquals("DELETE", close.method());
        assertEquals("/api/v1/sessions/42", close.path());
        assertEquals(Optional.of("Bearer " + TOKEN), close.authorization());
        assertEquals(new SessionStore.Missing(), store.load());
        assertEquals(Optional.empty(), active.client().token());
    }

    @Test
    public void logoutOfASessionTheServerAlreadyClosedIsNotAnError() throws IOException {
        server.reply("POST /api/v1/sessions", FakeServer.Reply.json(201, LOGIN_OK));
        server.reply("DELETE /api/v1/sessions/42", FakeServer.Reply.json(401, UNAUTHORIZED));
        Sessions.Active active = sessions.login(server.address(), PASSWORD);

        assertEquals(Optional.empty(), sessions.logout(active));
        assertEquals(new SessionStore.Missing(), store.load());
    }

    @Test
    public void logoutWithoutServerStillForgetsTheToken() throws IOException {
        server.reply("POST /api/v1/sessions", FakeServer.Reply.json(201, LOGIN_OK));
        Sessions.Active active = sessions.login(server.address(), PASSWORD);
        server.close();

        String warning = sessions.logout(active).orElseThrow();
        assertTrue(warning, warning.contains("no cerró"));
        assertFalse(warning, warning.contains(TOKEN));
        assertEquals(new SessionStore.Missing(), store.load());
    }

    @Test
    public void deviceNameIsTheComputerName() {
        assertEquals("SOBREMESA", Sessions.deviceName(Map.of("COMPUTERNAME", " SOBREMESA ")));
        assertEquals("Escritorio", Sessions.deviceName(Map.of("COMPUTERNAME", "  ")));
        assertEquals("Escritorio", Sessions.deviceName(Map.of()));
        assertEquals(64, Sessions.deviceName(Map.of("COMPUTERNAME", "ñ".repeat(100))).length());
    }
}
