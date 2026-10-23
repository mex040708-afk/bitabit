package io.github.mex040708afk.bitabit.desktop;

import io.github.mex040708afk.bitabit.core.ApiException;
import io.github.mex040708afk.bitabit.core.BitabitClient;
import io.github.mex040708afk.bitabit.core.Contract.Login;
import java.io.IOException;
import java.util.Map;
import java.util.Optional;

/**
 * Iniciar, retomar y cerrar la sesión con el servidor (ADR-025), sin nada de interfaz.
 * Todas las llamadas esperan a la red: se hacen desde un hilo de fondo (ADR-026).
 */
final class Sessions {

    static final String FALLBACK_DEVICE = "Escritorio";
    private static final int MAX_DEVICE_CHARS = 64;

    /** Una sesión abierta: el cliente ya lleva el token. */
    record Active(BitabitClient client, SavedSession saved) {

        @Override
        public String toString() {
            return "Active[" + saved + "]";
        }
    }

    /** Lo que pasa al arrancar la app. */
    sealed interface Resume {
    }

    /** La sesión guardada sigue valiendo. */
    record Resumed(Active session) implements Resume {
    }

    /**
     * Hay que pedir la contraseña.
     *
     * @param notice por qué, si había una sesión guardada que ya no vale
     * @param server el servidor de esa sesión, para no tener que escribirlo otra vez
     */
    record NeedsLogin(Optional<String> notice, Optional<String> server) implements Resume {
    }

    /** No se pudo comprobar la sesión: sigue guardada y se puede reintentar. */
    record Unreachable(String server, String message) implements Resume {
    }

    private final SessionStore store;
    private final String device;

    Sessions(SessionStore store, String device) {
        this.store = store;
        this.device = device;
    }

    /** El nombre con el que el equipo sale en la lista de sesiones: {@code COMPUTERNAME}, o «Escritorio». */
    static String deviceName(Map<String, String> env) {
        String name = env.getOrDefault("COMPUTERNAME", "").strip();
        if (name.isEmpty()) {
            return FALLBACK_DEVICE;
        }
        if (name.codePointCount(0, name.length()) > MAX_DEVICE_CHARS) {
            name = name.substring(0, name.offsetByCodePoints(0, MAX_DEVICE_CHARS));
        }
        return name;
    }

    /**
     * Inicia sesión y la guarda.
     *
     * @param server lo que escribió la persona; se valida con {@link ServerAddress#parse}
     * @throws IllegalArgumentException con un texto para enseñar, si la dirección o la
     *         contraseña no valen
     * @throws IOException si el servidor la rechaza ({@link ApiException}), si no responde o
     *         si no se pudo guardar
     */
    Active login(String server, String password) throws IOException {
        String address = ServerAddress.parse(server);
        if (password.isEmpty()) {
            throw new IllegalArgumentException("Escribe la contraseña.");
        }
        BitabitClient client = new BitabitClient(address);
        Login login = client.login(password, device);
        SavedSession saved = new SavedSession(address, login.getSession().getId(), login.getToken());
        try {
            store.save(saved);
        } catch (IOException e) {
            // Sin archivo, la sesión quedaría abierta en el servidor y sin nadie que la cierre.
            try {
                client.closeSession(saved.sessionId());
            } catch (IOException closing) {
                e.addSuppressed(closing);
            }
            throw e;
        }
        return new Active(client, saved);
    }

    /**
     * Retoma la sesión guardada, si la hay, y comprueba con el servidor que sigue valiendo.
     *
     * @throws IOException si la sesión no vale y no se puede borrar
     */
    Resume resume() throws IOException {
        SavedSession saved;
        switch (store.load()) {
            case SessionStore.Missing m -> {
                return new NeedsLogin(Optional.empty(), Optional.empty());
            }
            case SessionStore.Discarded d -> {
                return new NeedsLogin(Optional.of("Se ha borrado la sesión guardada porque " + d.reason()
                        + ". Vuelve a iniciar sesión."), Optional.empty());
            }
            case SessionStore.Found f -> saved = f.session();
        }
        BitabitClient client;
        try {
            client = new BitabitClient(saved.server());
            client.useToken(saved.token());
        } catch (IllegalArgumentException e) {
            // Core comprueba el formato del token y de la dirección: si no le valen, el
            // archivo está mal formado aunque sea JSON correcto.
            store.delete();
            return new NeedsLogin(Optional.of("Se ha borrado la sesión guardada porque está mal formada. "
                    + "Vuelve a iniciar sesión."), Optional.empty());
        }
        try {
            // La llamada con token más barata del contrato: unas pocas filas, sin la biblioteca.
            client.sessions();
        } catch (ApiException e) {
            if (e.status() == 401) {
                store.delete();
                return new NeedsLogin(Optional.of("La sesión guardada ya no vale en " + saved.server()
                        + ". Vuelve a iniciar sesión."), Optional.of(saved.server()));
            }
            return new Unreachable(saved.server(), Messages.request(saved.server(), e));
        } catch (IOException e) {
            return new Unreachable(saved.server(), Messages.request(saved.server(), e));
        }
        return new Resumed(new Active(client, saved));
    }

    /**
     * El servidor ha dejado de aceptar el token (401): se borra el archivo y se olvida el
     * token, sin llamar al servidor, que ya la dio por cerrada.
     *
     * @throws IOException si no se pudo borrar el archivo
     */
    void expire(Active session) throws IOException {
        session.client().forgetToken();
        store.delete();
    }

    /**
     * Cierra la sesión en el servidor y borra la guardada. El archivo se borra aunque el
     * servidor no responda: quien cierra sesión quiere que el token deje de estar aquí.
     *
     * @return un aviso si el servidor no la cerró
     * @throws IOException si no se pudo borrar el archivo
     */
    Optional<String> logout(Active session) throws IOException {
        Optional<String> warning = Optional.empty();
        try {
            session.client().closeSession(session.saved().sessionId());
        } catch (ApiException e) {
            // 401 o 404: el servidor ya la había cerrado, que es lo que se quería.
            if (e.status() != 401 && e.status() != 404) {
                warning = Optional.of("El servidor no cerró la sesión: " + Messages.request(session.saved().server(), e)
                        + " Ciérrala desde otro dispositivo si hace falta.");
            }
        } catch (IOException e) {
            warning = Optional.of("El servidor no cerró la sesión: " + Messages.request(session.saved().server(), e)
                    + " Ciérrala desde otro dispositivo si hace falta.");
        }
        session.client().forgetToken();
        store.delete();
        return warning;
    }
}
