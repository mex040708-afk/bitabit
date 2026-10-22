package io.github.mex040708afk.bitabit.desktop;

import java.net.URI;
import java.net.URISyntaxException;
import java.util.Locale;

/** La dirección del servidor que escribe la persona: {@code http://host:puerto}. */
final class ServerAddress {

    private static final int MAX_CHARS = 2048;

    private ServerAddress() {
    }

    /**
     * La dirección escrita, como {@code esquema://host[:puerto]}, sin barra final.
     *
     * @throws IllegalArgumentException con un texto para enseñar, si no es http(s) a un
     *         servidor sin más: core resuelve /api y /media contra la raíz, y una ruta o
     *         unas credenciales en la dirección se perderían o acabarían en los registros
     */
    static String parse(String typed) {
        String text = typed.strip();
        if (text.isEmpty()) {
            throw new IllegalArgumentException("Escribe la dirección del servidor, por ejemplo http://servidor:8080.");
        }
        if (text.length() > MAX_CHARS) {
            throw new IllegalArgumentException("La dirección del servidor es demasiado larga.");
        }
        URI uri;
        try {
            uri = new URI(text);
        } catch (URISyntaxException e) {
            throw new IllegalArgumentException("La dirección del servidor no es válida: tiene que ser http://host:puerto.", e);
        }
        String scheme = uri.getScheme() == null ? "" : uri.getScheme().toLowerCase(Locale.ROOT);
        if (!scheme.equals("http") && !scheme.equals("https")) {
            throw new IllegalArgumentException("La dirección tiene que empezar por http:// o https://.");
        }
        if (uri.getHost() == null || uri.getHost().isEmpty()) {
            throw new IllegalArgumentException("Falta el nombre o la IP del servidor: http://host:puerto.");
        }
        if (uri.getRawUserInfo() != null) {
            throw new IllegalArgumentException("La dirección no puede llevar usuario ni contraseña.");
        }
        if (uri.getPort() == 0 || uri.getPort() > 65535) {
            throw new IllegalArgumentException("El puerto tiene que ser de 1 a 65535.");
        }
        String path = uri.getRawPath();
        if ((path != null && !path.isEmpty() && !path.equals("/")) || uri.getRawQuery() != null
                || uri.getRawFragment() != null) {
            throw new IllegalArgumentException("Escribe solo el servidor, sin ruta: http://host:puerto.");
        }
        String host = uri.getHost().toLowerCase(Locale.ROOT);
        return scheme + "://" + host + (uri.getPort() == -1 ? "" : ":" + uri.getPort());
    }
}
