package io.github.mex040708afk.bitabit;

import android.content.res.Resources;
import io.github.mex040708afk.bitabit.core.ApiException;
import java.io.IOException;
import java.util.Optional;

/** Convierte los errores de las llamadas en el texto que ve la persona. */
final class ErrorMessages {

    private ErrorMessages() {
    }

    /** Un texto por escribir: un recurso con sus argumentos, sin depender aún de {@link Resources}. */
    static final class Message {

        final int resId;
        final boolean plural;
        final int quantity;
        final Object[] args;

        private Message(int resId, boolean plural, int quantity, Object... args) {
            this.resId = resId;
            this.plural = plural;
            this.quantity = quantity;
            this.args = args;
        }

        static Message of(int resId, Object... args) {
            return new Message(resId, false, 0, args);
        }

        static Message plural(int resId, int quantity) {
            return new Message(resId, true, quantity, quantity);
        }

        String format(Resources resources) {
            return plural ? resources.getQuantityString(resId, quantity, args) : resources.getString(resId, args);
        }
    }

    static Message describe(Exception e) {
        if (e instanceof ApiException) {
            ApiException api = (ApiException) e;
            return describeApi(api.status(), api.code(), api.attemptsLeft(), api.retryAfter());
        }
        if (e instanceof IncompatibleServerException) {
            return Message.of(R.string.error_incompatible, ((IncompatibleServerException) e).apiVersion());
        }
        if (e instanceof IllegalArgumentException) {
            return Message.of(R.string.error_invalid, detail(e));
        }
        if (e instanceof IOException) {
            return Message.of(R.string.error_network, detail(e));
        }
        return Message.of(R.string.error_unexpected, detail(e));
    }

    /** Los errores del contrato (ApiException), por separado para probarlos sin servidor. */
    static Message describeApi(int status, String code, Optional<Integer> attemptsLeft, Optional<Integer> retryAfter) {
        if (status == 401 && ("wrong_password".equals(code) || attemptsLeft.isPresent())) {
            return attemptsLeft.isPresent()
                    ? Message.plural(R.plurals.error_wrong_password_attempts, attemptsLeft.get())
                    : Message.of(R.string.error_wrong_password);
        }
        if (status == 401) {
            return Message.of(R.string.error_unauthorized);
        }
        if (status == 429) {
            return retryAfter.isPresent()
                    ? Message.plural(R.plurals.error_blocked_seconds, retryAfter.get())
                    : Message.of(R.string.error_blocked);
        }
        return Message.of(R.string.error_server, status, code.isEmpty() ? "?" : code);
    }

    /** Si el error es el 401 de un token que el servidor ya no acepta. */
    static boolean isSessionRejected(Exception e) {
        return e instanceof ApiException && ((ApiException) e).status() == 401;
    }

    private static String detail(Exception e) {
        String message = e.getMessage();
        return message == null || message.trim().isEmpty() ? e.getClass().getSimpleName() : message;
    }
}
