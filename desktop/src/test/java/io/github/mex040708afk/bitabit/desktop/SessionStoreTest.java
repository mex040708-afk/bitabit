package io.github.mex040708afk.bitabit.desktop;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.stream.Stream;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.TemporaryFolder;

public class SessionStoreTest {

    /** Inventado, con el formato del contrato: 43 caracteres de base64url. */
    static final String TOKEN = "inventado-0123456789abcdefghijklmnopqrstuvw";

    @Rule
    public final TemporaryFolder temp = new TemporaryFolder();

    private Path dir() {
        return temp.getRoot().toPath().resolve("Bitabit");
    }

    @Test
    public void savesAndLoadsBack() throws IOException {
        SessionStore store = new SessionStore(dir());
        SavedSession saved = new SavedSession("http://servidor:8080", 7, TOKEN);
        store.save(saved);
        assertEquals(new SessionStore.Found(saved), store.load());
        // Un archivo nuevo reemplaza al anterior.
        SavedSession other = new SavedSession("https://otro", 8, TOKEN.replace('a', 'b'));
        store.save(other);
        assertEquals(new SessionStore.Found(other), new SessionStore(dir()).load());
    }

    @Test
    public void nothingSavedIsMissing() throws IOException {
        assertEquals(new SessionStore.Missing(), new SessionStore(dir()).load());
    }

    @Test
    public void neverSavesThePassword() throws IOException {
        SessionStore store = new SessionStore(dir());
        store.save(new SavedSession("http://servidor:8080", 7, TOKEN));
        String text = Files.readString(store.file(), StandardCharsets.UTF_8);
        assertTrue(text, text.contains("\"server\":\"http://servidor:8080\""));
        assertTrue(text, text.contains("\"sessionId\":7"));
        assertFalse(text, text.contains("password"));
    }

    @Test
    public void leavesNoTemporaryFileBehind() throws IOException {
        SessionStore store = new SessionStore(dir());
        store.save(new SavedSession("http://servidor:8080", 7, TOKEN));
        store.save(new SavedSession("http://servidor:8080", 9, TOKEN));
        try (Stream<Path> files = Files.list(dir())) {
            assertEquals(List.of(store.file()), files.toList());
        }
    }

    @Test
    public void aFailedSaveLeavesNoTemporaryFileAndKeepsTheOldOne() throws IOException {
        SessionStore store = new SessionStore(dir());
        SavedSession saved = new SavedSession("http://servidor:8080", 7, TOKEN);
        store.save(saved);
        // Un directorio con el nombre del archivo: el rename no puede reemplazarlo.
        Path blockedDir = temp.getRoot().toPath().resolve("Bloqueado");
        Files.createDirectories(blockedDir.resolve(SessionStore.FILE_NAME));
        Files.writeString(blockedDir.resolve(SessionStore.FILE_NAME).resolve("x"), "x");
        SessionStore blocked = new SessionStore(blockedDir);
        try {
            blocked.save(saved);
            fail("no debería poder guardar encima de un directorio");
        } catch (IOException e) {
            assertFalse(e.getMessage(), e.getMessage().contains(TOKEN));
        }
        try (Stream<Path> files = Files.list(blockedDir)) {
            assertEquals(List.of(blockedDir.resolve(SessionStore.FILE_NAME)), files.toList());
        }
        assertEquals(new SessionStore.Found(saved), store.load());
    }

    @Test
    public void deletes() throws IOException {
        SessionStore store = new SessionStore(dir());
        store.save(new SavedSession("http://servidor:8080", 7, TOKEN));
        store.delete();
        assertFalse(Files.exists(store.file()));
        assertEquals(new SessionStore.Missing(), store.load());
        store.delete();
    }

    @Test
    public void aMalformedFileIsDeletedAndExplained() throws IOException {
        for (String bad : new String[] {
                "",
                "no es json " + TOKEN,
                "[]",
                "{\"server\":\"http://servidor:8080\",\"sessionId\":7}",
                "{\"server\":\"http://servidor:8080\",\"token\":\"" + TOKEN + "\"}",
                "{\"sessionId\":7,\"token\":\"" + TOKEN + "\"}",
                "{\"server\":\"http://servidor:8080\",\"sessionId\":\"7\",\"token\":\"" + TOKEN + "\"}",
                "{\"server\":\"http://servidor:8080\",\"sessionId\":0,\"token\":\"" + TOKEN + "\"}",
                "{\"server\":\"http://servidor:8080\",\"sessionId\":7.5,\"token\":\"" + TOKEN + "\"}",
                "{\"server\":\"http://servidor:8080\",\"sessionId\":99999999999999999999,\"token\":\"" + TOKEN + "\"}",
                "{\"server\":\"ftp://servidor\",\"sessionId\":7,\"token\":\"" + TOKEN + "\"}",
                "{\"server\":\"http://servidor:8080\",\"sessionId\":7,\"token\":\"\"}",
                "{\"server\":\"http://servidor:8080\",\"sessionId\":7,\"token\":7}",
                "{\"server\":\"http://servidor:8080\",\"sessionId\":7,\"token\":\"" + TOKEN.repeat(200) + "\"}",
        }) {
            SessionStore store = new SessionStore(dir());
            Files.createDirectories(dir());
            Files.writeString(store.file(), bad, StandardCharsets.UTF_8);
            SessionStore.Stored stored = store.load();
            assertTrue(bad, stored instanceof SessionStore.Discarded);
            String reason = ((SessionStore.Discarded) stored).reason();
            assertFalse(reason, reason.contains(TOKEN));
            assertFalse(bad, Files.exists(store.file()));
        }
    }

    @Test
    public void anUnreadableFileIsDeletedAndExplained() throws IOException {
        // Un directorio en el sitio del archivo: no se puede leer, pero sí borrar.
        SessionStore store = new SessionStore(dir());
        Files.createDirectories(store.file());
        SessionStore.Stored stored = store.load();
        assertTrue(stored.toString(), stored instanceof SessionStore.Discarded);
        assertFalse(Files.exists(store.file()));
    }

    @Test
    public void toStringHidesTheToken() {
        SavedSession saved = new SavedSession("http://servidor:8080", 7, TOKEN);
        assertFalse(saved.toString(), saved.toString().contains(TOKEN));
        assertTrue(saved.toString(), saved.toString().contains("http://servidor:8080"));
        SessionStore.Found found = new SessionStore.Found(saved);
        assertFalse(found.toString(), found.toString().contains(TOKEN));
    }
}
