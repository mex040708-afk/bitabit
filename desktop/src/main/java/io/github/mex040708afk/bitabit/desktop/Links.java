package io.github.mex040708afk.bitabit.desktop;

import java.net.URI;
import java.net.URISyntaxException;
import java.util.Locale;
import java.util.Optional;

/** Los enlaces firmados que imprime «bitabit link». */
final class Links {

    private Links() {
    }

    /** El enlace pegado, si es una URL http(s) a /media/ con su firma. */
    static Optional<URI> parse(String pasted) {
        URI uri;
        try {
            uri = new URI(pasted.trim());
        } catch (URISyntaxException e) {
            return Optional.empty();
        }
        String scheme = uri.getScheme();
        if (!"http".equals(scheme) && !"https".equals(scheme)) {
            return Optional.empty();
        }
        if (uri.getHost() == null || uri.getPath() == null || !uri.getPath().startsWith("/media/")
                || uri.getRawQuery() == null) {
            return Optional.empty();
        }
        return Optional.of(uri);
    }

    /** Lo que se puede enseñar o escribir del enlace: la ruta, sin la firma. */
    static String safe(URI link) {
        return link.getPath();
    }

    /** La extensión de la ruta, en minúsculas: {@code flac}, {@code m4a}… */
    static String extension(URI link) {
        String path = link.getPath();
        int dot = path.lastIndexOf('.');
        return dot < 0 ? "" : path.substring(dot + 1).toLowerCase(Locale.ROOT);
    }
}
