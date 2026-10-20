package io.github.mex040708afk.bitabit.desktop;

import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import java.util.Optional;

/**
 * Un formato de audio tal como lo cuenta mpv en las propiedades
 * {@code audio-params} (lo que da el decodificador) y {@code audio-out-params}
 * (lo que se manda a la salida de audio).
 *
 * @param format el formato de muestra de mpv: {@code s16}, {@code s32}, {@code float}…
 */
record AudioParams(String format, int sampleRate, int channels) {

    /** Lee el valor de la propiedad, que no existe mientras no hay audio. */
    static Optional<AudioParams> from(JsonElement data) {
        if (data == null || !data.isJsonObject()) {
            return Optional.empty();
        }
        JsonObject o = data.getAsJsonObject();
        if (!o.has("format") || !o.has("samplerate") || !o.has("channel-count")) {
            return Optional.empty();
        }
        return Optional.of(new AudioParams(
                o.get("format").getAsString(),
                o.get("samplerate").getAsInt(),
                o.get("channel-count").getAsInt()));
    }

    String describe() {
        return format + ", " + sampleRate + " Hz, " + channels + (channels == 1 ? " canal" : " canales");
    }
}
