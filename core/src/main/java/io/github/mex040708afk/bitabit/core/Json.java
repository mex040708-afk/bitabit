package io.github.mex040708afk.bitabit.core;

import com.google.gson.Gson;
import com.google.gson.GsonBuilder;

/**
 * Lectura y escritura del JSON de la API. Gson ignora los campos que no conoce, como
 * pide el contrato: así una versión nueva del servidor, con campos de más, no rompe a
 * los clientes.
 */
public final class Json {

    private static final Gson GSON = new GsonBuilder().create();

    private Json() {
    }

    /**
     * Convierte el cuerpo de una respuesta en el modelo {@code type}.
     *
     * @throws IllegalArgumentException si el cuerpo está vacío: el contrato siempre
     *         manda un objeto, así que un cuerpo vacío es un error del servidor.
     */
    public static <T> T fromJson(String body, Class<T> type) {
        T value = GSON.fromJson(body, type);
        if (value == null) {
            throw new IllegalArgumentException("cuerpo vacío: se esperaba " + type.getSimpleName());
        }
        return value;
    }

    /** Escribe un modelo como JSON, para el cuerpo de una petición. */
    public static String toJson(Object value) {
        return GSON.toJson(value);
    }
}
