package io.github.mex040708afk.bitabit.desktop;

/**
 * Lo que se guarda de una sesión entre arranques (ADR-025). Nunca la contraseña.
 *
 * @param server dirección normalizada por {@link ServerAddress#parse}
 * @param sessionId el id de la sesión en el servidor, para cerrarla
 * @param token el token de {@code Authorization: Bearer}
 */
record SavedSession(String server, long sessionId, String token) {

    /** Sin el token: el registro acaba en mensajes y depuradores. */
    @Override
    public String toString() {
        return "SavedSession[server=" + server + ", sessionId=" + sessionId + ", token=(oculto)]";
    }
}
