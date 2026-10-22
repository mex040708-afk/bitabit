package io.github.mex040708afk.bitabit.desktop;

import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParseException;
import com.google.gson.JsonParser;
import com.google.gson.JsonPrimitive;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.AtomicMoveNotSupportedException;
import java.nio.file.Files;
import java.nio.file.NoSuchFileException;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.util.Optional;

/**
 * El archivo {@code session.json} con la sesión guardada (ADR-025). Los motivos y los
 * errores nunca llevan el contenido del archivo, porque ahí está el token.
 */
final class SessionStore {

    static final String FILE_NAME = "session.json";

    /** Un archivo legítimo ocupa unos 150 bytes: más es que no es nuestro. */
    private static final long MAX_BYTES = 4096;

    /** Lo que había guardado. */
    sealed interface Stored {
    }

    /** No hay sesión guardada. */
    record Missing() implements Stored {
    }

    record Found(SavedSession session) implements Stored {
    }

    /** Había un archivo que no valía y se ha borrado. {@code reason} se puede enseñar. */
    record Discarded(String reason) implements Stored {
    }

    private final Path dir;
    private final Path file;

    /** @param dir la carpeta de la app; en Windows, {@code %APPDATA%\Bitabit} */
    SessionStore(Path dir) {
        this.dir = dir;
        this.file = dir.resolve(FILE_NAME);
    }

    /** La carpeta de la app en este equipo: {@code %APPDATA%\Bitabit}, o {@code ~/.bitabit} sin APPDATA. */
    static Path defaultDir() {
        String appData = System.getenv("APPDATA");
        if (appData != null && !appData.isBlank()) {
            return Path.of(appData, "Bitabit");
        }
        return Path.of(System.getProperty("user.home"), ".bitabit");
    }

    Path file() {
        return file;
    }

    /**
     * Lee la sesión guardada. Si el archivo no vale, lo borra y devuelve {@link Discarded}.
     *
     * @throws IOException si el archivo no vale y tampoco se puede borrar
     */
    Stored load() throws IOException {
        byte[] bytes;
        try {
            if (Files.size(file) > MAX_BYTES) {
                return discard("el archivo de la sesión es demasiado grande");
            }
            bytes = Files.readAllBytes(file);
        } catch (NoSuchFileException e) {
            return new Missing();
        } catch (IOException e) {
            // El motivo del sistema dice qué pasó con el archivo, no lo que contiene.
            return discard("no se pudo leer el archivo de la sesión (" + e.getClass().getSimpleName() + ")");
        }
        Optional<SavedSession> session = parse(new String(bytes, StandardCharsets.UTF_8));
        if (session.isEmpty()) {
            return discard("el archivo de la sesión está mal formado");
        }
        return new Found(session.get());
    }

    /** Guarda la sesión de forma atómica: o queda la anterior entera, o la nueva entera. */
    void save(SavedSession session) throws IOException {
        JsonObject json = new JsonObject();
        json.addProperty("server", session.server());
        json.addProperty("sessionId", session.sessionId());
        json.addProperty("token", session.token());
        Files.createDirectories(dir);
        // En la misma carpeta: un rename entre volúmenes no puede ser atómico.
        Path temp = Files.createTempFile(dir, "session-", ".tmp");
        try {
            Files.writeString(temp, json.toString(), StandardCharsets.UTF_8);
            Files.move(temp, file, StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING);
        } catch (AtomicMoveNotSupportedException e) {
            deleteTemp(temp, e);
            throw new IOException("el sistema de archivos de " + dir + " no permite guardar la sesión de forma atómica", e);
        } catch (IOException e) {
            deleteTemp(temp, e);
            throw new IOException("no se pudo guardar la sesión en " + file, e);
        }
    }

    /** Borra la sesión guardada, si la hay. */
    void delete() throws IOException {
        try {
            Files.deleteIfExists(file);
        } catch (IOException e) {
            throw new IOException("no se pudo borrar la sesión guardada en " + file, e);
        }
    }

    private Discarded discard(String reason) throws IOException {
        delete();
        return new Discarded(reason);
    }

    private static void deleteTemp(Path temp, IOException failure) {
        try {
            Files.deleteIfExists(temp);
        } catch (IOException e) {
            failure.addSuppressed(e);
        }
    }

    /** La sesión del texto del archivo, o vacío si le falta algo o algo no tiene su tipo. */
    private static Optional<SavedSession> parse(String text) {
        JsonObject json;
        try {
            JsonElement root = JsonParser.parseString(text);
            if (!root.isJsonObject()) {
                return Optional.empty();
            }
            json = root.getAsJsonObject();
        } catch (JsonParseException e) {
            return Optional.empty();
        }
        Optional<String> server = string(json, "server");
        Optional<String> token = string(json, "token");
        JsonElement id = json.get("sessionId");
        if (server.isEmpty() || token.isEmpty() || token.get().isEmpty() || id == null || !id.isJsonPrimitive()
                || !id.getAsJsonPrimitive().isNumber()) {
            return Optional.empty();
        }
        long sessionId;
        try {
            sessionId = id.getAsJsonPrimitive().getAsBigDecimal().longValueExact();
        } catch (ArithmeticException e) {
            return Optional.empty();
        }
        if (sessionId < 1) {
            return Optional.empty();
        }
        String normalized;
        try {
            normalized = ServerAddress.parse(server.get());
        } catch (IllegalArgumentException e) {
            return Optional.empty();
        }
        return Optional.of(new SavedSession(normalized, sessionId, token.get()));
    }

    private static Optional<String> string(JsonObject json, String name) {
        JsonElement value = json.get(name);
        if (value instanceof JsonPrimitive p && p.isString()) {
            return Optional.of(p.getAsString());
        }
        return Optional.empty();
    }
}
