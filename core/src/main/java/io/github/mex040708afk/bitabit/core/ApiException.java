package io.github.mex040708afk.bitabit.core;

import com.google.gson.JsonParseException;
import io.github.mex040708afk.bitabit.core.Contract.ErrorBody;
import java.io.IOException;
import java.util.Optional;

/**
 * El servidor ha respondido con un error. {@link #code()} es el código del contrato:
 * {@code bad_request}, {@code unauthorized}, {@code wrong_password}, {@code blocked},
 * {@code not_found} o {@code internal}. Es vacío si el cuerpo del error no se pudo
 * leer; en ese caso, el estado HTTP es lo único fiable.
 */
public final class ApiException extends IOException {

    private static final long serialVersionUID = 1L;

    private final int status;
    private final String code;
    private final Integer attemptsLeft;
    private final Integer retryAfter;

    private ApiException(int status, String code, String message, Integer attemptsLeft, Integer retryAfter,
            Throwable cause) {
        super(message, cause);
        this.status = status;
        this.code = code;
        this.attemptsLeft = attemptsLeft;
        this.retryAfter = retryAfter;
    }

    /** El error de una respuesta que no es 2xx, leído de su cuerpo. */
    static ApiException fromResponse(int status, String body) {
        try {
            ErrorBody error = Json.fromJson(body, ErrorBody.class);
            String code = error.getError() == null ? "" : error.getError();
            String message = error.getMessage() == null ? "" : error.getMessage();
            return new ApiException(status, code, message,
                    error.getAttemptsLeft().orElse(null), error.getRetryAfter().orElse(null), null);
        } catch (JsonParseException | IllegalArgumentException e) {
            return new ApiException(status, "", "respuesta de error ilegible", null, null, e);
        }
    }

    /** El estado HTTP de la respuesta. */
    public int status() {
        return status;
    }

    /** El código del contrato, o vacío si el cuerpo del error no se pudo leer. */
    public String code() {
        return code;
    }

    /** En un 401 de contraseña equivocada, cuántos intentos quedan antes del bloqueo. */
    public Optional<Integer> attemptsLeft() {
        return Optional.ofNullable(attemptsLeft);
    }

    /** En un 429 por bloqueo, cuántos segundos faltan para que acabe. */
    public Optional<Integer> retryAfter() {
        return Optional.ofNullable(retryAfter);
    }
}
