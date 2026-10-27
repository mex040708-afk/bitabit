package io.github.mex040708afk.bitabit;

import java.io.IOException;

/** El servidor responde, pero con una versión de la API que esta app no entiende. */
final class IncompatibleServerException extends IOException {

    private static final long serialVersionUID = 1L;

    private final String apiVersion;

    IncompatibleServerException(String apiVersion) {
        super("versión de la API no compatible: " + apiVersion);
        this.apiVersion = apiVersion;
    }

    String apiVersion() {
        return apiVersion;
    }
}
