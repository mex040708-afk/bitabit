package io.github.mex040708afk.bitabit.desktop;

/** Por qué terminó una pista: el campo {@code reason} del evento {@code end-file} de mpv. */
enum EndReason {
    /** Sonó entera: la cola puede pasar a la siguiente. */
    EOF,
    /** Se paró, o otra pista la sustituyó con {@code loadfile … replace}. */
    STOP,
    /** mpv está saliendo. */
    QUIT,
    /** No se pudo abrir o decodificar. */
    ERROR,
    /** Era una lista o una redirección que mpv sustituyó por otra entrada. */
    REDIRECT,
    /** Un motivo que esta versión de Bitabit no conoce. */
    UNKNOWN;

    static EndReason from(String reason) {
        return switch (reason) {
            case "eof" -> EOF;
            case "stop" -> STOP;
            case "quit" -> QUIT;
            case "error" -> ERROR;
            case "redirect" -> REDIRECT;
            default -> UNKNOWN;
        };
    }
}
