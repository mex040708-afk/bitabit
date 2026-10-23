package io.github.mex040708afk.bitabit.desktop;

import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import java.io.IOException;
import java.util.List;
import java.util.Optional;
import java.util.OptionalDouble;

/**
 * Traduce lo que manda mpv a llamadas del {@link Player.Listener}. Está aparte
 * del {@link Player} para probarla con JSON de ejemplo, sin mpv.
 */
final class PlayerEvents implements Mpv.Listener {

    /** Las propiedades que observa el Player; sus cambios llegan con el nombre. */
    static final List<String> PROPERTIES = List.of("time-pos", "duration", "pause", "audio-params", "audio-out-params");

    private final Player.Listener listener;

    PlayerEvents(Player.Listener listener) {
        this.listener = listener;
    }

    @Override
    public void onEvent(JsonObject event) {
        switch (event.get("event").getAsString()) {
            case "property-change" -> property(event.get("name").getAsString(), event.get("data"));
            case "end-file" -> listener.onTrackEnded(
                    EndReason.from(string(event, "reason").orElse("")),
                    string(event, "file_error"));
            default -> {
                // El Player no necesita el resto: start-file, file-loaded, idle…
            }
        }
    }

    @Override
    public void onClosed(Optional<IOException> error) {
        IOException failure = new IOException("mpv se cerró sin pedírselo");
        error.ifPresent(failure::initCause);
        listener.onFailure(failure);
    }

    /** Sin {@code data}, la propiedad no está disponible: por ejemplo, sin pista cargada. */
    private void property(String name, JsonElement data) {
        switch (name) {
            case "time-pos" -> listener.onPosition(number(data));
            case "duration" -> listener.onDuration(number(data));
            case "pause" -> {
                if (data != null && data.isJsonPrimitive() && data.getAsJsonPrimitive().isBoolean()) {
                    listener.onPause(data.getAsBoolean());
                }
            }
            case "audio-params" -> listener.onDecodedFormat(AudioParams.from(data));
            case "audio-out-params" -> listener.onOutputFormat(AudioParams.from(data));
            default -> {
                // Solo llegan las que observa el Player.
            }
        }
    }

    private static OptionalDouble number(JsonElement data) {
        if (data == null || !data.isJsonPrimitive() || !data.getAsJsonPrimitive().isNumber()) {
            return OptionalDouble.empty();
        }
        return OptionalDouble.of(data.getAsDouble());
    }

    private static Optional<String> string(JsonObject event, String field) {
        JsonElement value = event.get(field);
        if (value == null || !value.isJsonPrimitive()) {
            return Optional.empty();
        }
        return Optional.of(value.getAsString());
    }
}
