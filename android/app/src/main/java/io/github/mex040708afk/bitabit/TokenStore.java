package io.github.mex040708afk.bitabit;

import android.content.SharedPreferences;
import java.nio.charset.StandardCharsets;
import java.security.GeneralSecurityException;
import java.security.ProviderException;
import java.util.Base64;
import java.util.Optional;
import java.util.function.Consumer;

/**
 * La sesión guardada entre arranques (ADR-021): el servidor, el ID de la sesión y el token
 * cifrado con {@link TokenCipher}, en unas preferencias privadas que no entran en las
 * copias de seguridad. La contraseña nunca se guarda.
 *
 * <p>Si el token no se puede descifrar, se borra todo y la app vuelve a pedir la
 * contraseña: es lo que pasa si el Keystore pierde la clave o la invalida.
 */
final class TokenStore {

    /** Nombre del archivo de preferencias; las reglas de copia de seguridad lo excluyen. */
    static final String PREFS_NAME = "bitabit_session";

    private static final String KEY_SERVER = "server";
    private static final String KEY_SESSION_ID = "session_id";
    private static final String KEY_TOKEN = "token";

    private final SharedPreferences prefs;
    private final TokenCipher cipher;
    private final Consumer<String> warn;

    /**
     * @param warn recibe los avisos de lo que se ha descartado y por qué; nunca llevan el
     *        token
     */
    TokenStore(SharedPreferences prefs, TokenCipher cipher, Consumer<String> warn) {
        this.prefs = prefs;
        this.cipher = cipher;
        this.warn = warn;
    }

    /** Guarda la sesión. Si no se puede cifrar, no guarda nada. */
    void save(String server, long sessionId, String token) throws GeneralSecurityException {
        byte[] sealed;
        try {
            sealed = cipher.encrypt(token.getBytes(StandardCharsets.UTF_8));
        } catch (ProviderException e) {
            // Algunos Keystore fallan con esta excepción sin comprobar en lugar de la de la firma.
            throw new GeneralSecurityException("el Keystore no pudo cifrar el token", e);
        }
        prefs.edit()
                .putString(KEY_SERVER, server)
                .putLong(KEY_SESSION_ID, sessionId)
                .putString(KEY_TOKEN, Base64.getEncoder().encodeToString(sealed))
                .apply();
    }

    /** La sesión guardada, o vacío si no hay o no se pudo descifrar (en ese caso, se borra). */
    Optional<SavedSession> load() {
        String sealed = prefs.getString(KEY_TOKEN, null);
        if (sealed == null) {
            return Optional.empty();
        }
        String server = prefs.getString(KEY_SERVER, null);
        long sessionId = prefs.getLong(KEY_SESSION_ID, 0);
        if (server == null || sessionId < 1) {
            discard("la sesión guardada está incompleta");
            return Optional.empty();
        }
        byte[] plain;
        try {
            plain = cipher.decrypt(Base64.getDecoder().decode(sealed));
        } catch (GeneralSecurityException | ProviderException | IllegalArgumentException e) {
            discard("no se pudo descifrar el token (" + e.getClass().getSimpleName() + ")");
            return Optional.empty();
        }
        return Optional.of(new SavedSession(server, sessionId, new String(plain, StandardCharsets.UTF_8)));
    }

    /** El último servidor con el que se inició sesión, para no tener que escribirlo otra vez. */
    Optional<String> lastServer() {
        return Optional.ofNullable(prefs.getString(KEY_SERVER, null));
    }

    /** Borra la sesión guardada. El servidor se queda, porque no es secreto y ahorra escribirlo. */
    void clear() {
        prefs.edit().remove(KEY_TOKEN).remove(KEY_SESSION_ID).apply();
    }

    private void discard(String reason) {
        warn.accept(reason + "; se borra la sesión guardada");
        clear();
        try {
            cipher.reset();
        } catch (GeneralSecurityException | ProviderException e) {
            // La sesión ya está borrada; la clave vieja solo se reutilizaría para cifrar.
            warn.accept("no se pudo borrar la clave del token (" + e.getClass().getSimpleName() + ")");
        }
    }

    /** Una sesión guardada. No tiene {@code toString()} propio para que el token no acabe en un registro. */
    static final class SavedSession {

        private final String server;
        private final long sessionId;
        private final String token;

        SavedSession(String server, long sessionId, String token) {
            this.server = server;
            this.sessionId = sessionId;
            this.token = token;
        }

        String server() {
            return server;
        }

        long sessionId() {
            return sessionId;
        }

        String token() {
            return token;
        }
    }
}
