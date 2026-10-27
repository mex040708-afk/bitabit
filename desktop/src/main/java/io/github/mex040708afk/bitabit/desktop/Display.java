package io.github.mex040708afk.bitabit.desktop;

import io.github.mex040708afk.bitabit.core.Contract.Album;
import io.github.mex040708afk.bitabit.core.Contract.Track;
import io.github.mex040708afk.bitabit.core.Contract.TrackFile;
import java.util.List;
import java.util.Locale;

/** Cómo se escriben los datos del catálogo en pantalla. Sin JavaFX, para poder probarlo. */
final class Display {

    /** Lo que se pinta (en cursiva) cuando el servidor trae un nombre vacío. */
    static final String UNNAMED = "(sin nombre)";

    private Display() {
    }

    /** Una fila de la tabla de pistas. */
    record TrackRow(String number, String title, String duration, String format) {
    }

    static boolean isBlank(String text) {
        return text == null || text.isBlank();
    }

    /** El nombre tal cual, o {@link #UNNAMED} si está vacío. */
    static String name(String text) {
        return isBlank(text) ? UNNAMED : text.strip();
    }

    /** {@code m:ss}, o {@code h:mm:ss} desde la hora. Los milisegundos sobrantes se descartan, como en los reproductores. */
    static String duration(long millis) {
        if (millis < 0) {
            return "–";
        }
        long total = millis / 1000;
        long hours = total / 3600;
        long minutes = total % 3600 / 60;
        long seconds = total % 60;
        return hours > 0
                ? String.format(Locale.ROOT, "%d:%02d:%02d", hours, minutes, seconds)
                : String.format(Locale.ROOT, "%d:%02d", minutes, seconds);
    }

    /** {@code 1 álbum}, {@code 3 álbumes}. */
    static String count(int n, String singular, String plural) {
        return n + " " + (n == 1 ? singular : plural);
    }

    /**
     * La versión original de la pista: {@code FLAC 24/96} si no tiene pérdida y se conocen
     * los bits, {@code MP3 44.1 kHz} si no. Vacío si la pista no trae versiones.
     */
    static String format(List<TrackFile> files) {
        if (files == null || files.isEmpty()) {
            return "";
        }
        TrackFile file = files.get(0);
        String codec = isBlank(file.getCodec()) ? "?" : file.getCodec().strip().toUpperCase(Locale.ROOT);
        String rate = kilohertz(file.getSampleRate());
        if (file.isLossless() && file.getBits().isPresent()) {
            return codec + " " + file.getBits().get() + "/" + rate;
        }
        return codec + " " + rate + " kHz";
    }

    private static String kilohertz(int sampleRate) {
        String text = String.valueOf(sampleRate / 1000.0);
        return text.endsWith(".0") ? text.substring(0, text.length() - 2) : text;
    }

    /** Las filas del álbum. Con más de un disco, el número lleva el disco: {@code 2-05}. */
    static List<TrackRow> rows(Album album) {
        boolean multiDisc = album.getTracks().stream()
                .anyMatch(track -> track.getDiscNumber().orElse(1) > 1);
        return album.getTracks().stream().map(track -> row(track, multiDisc)).toList();
    }

    private static TrackRow row(Track track, boolean multiDisc) {
        String number = track.getTrackNumber().map(String::valueOf).orElse("–");
        if (multiDisc && track.getTrackNumber().isPresent()) {
            number = track.getDiscNumber().orElse(1) + "-"
                    + String.format(Locale.ROOT, "%02d", track.getTrackNumber().get());
        }
        return new TrackRow(number, name(track.getTitle()), duration(track.getDurationMs()), format(track.getFiles()));
    }
}
