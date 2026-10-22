package io.github.mex040708afk.bitabit.core;

import io.github.mex040708afk.bitabit.core.BitabitClient.Quality;
import io.github.mex040708afk.bitabit.core.Contract.Track;
import io.github.mex040708afk.bitabit.core.Contract.TrackFile;
import java.util.Objects;

/**
 * Qué versión de una pista pedir según la red y lo que prefiere el usuario (ADR-022).
 *
 * <p>Solo elige entre las versiones que la pista ya tiene: nunca pide una conversión.
 * Si falta la versión elegida, usa la otra; por ejemplo, una pista que ya tenía
 * pérdida solo tiene la original, sin AAC de 256 kbit/s.
 */
public final class QualityPolicy {

    /** El tipo de red por la que se va a escuchar. */
    public enum Network {
        /** Sin límite de datos, como una wifi doméstica. */
        UNMETERED,
        /** Con datos medidos, como la red móvil. */
        METERED,
        /** No se sabe; se trata como medida. */
        UNKNOWN
    }

    /** Lo que el usuario ha elegido en los ajustes. */
    public enum Preference {
        /** La original sin límite de datos y la AAC en el resto. */
        AUTO,
        ALWAYS_ORIGINAL,
        ALWAYS_AAC
    }

    private QualityPolicy() {
    }

    /**
     * La versión de {@code track} que conviene pedir.
     *
     * @throws IllegalArgumentException si la pista no tiene ninguna versión conocida
     */
    public static Quality choose(Track track, Network network, Preference preference) {
        Objects.requireNonNull(track, "track");
        Objects.requireNonNull(network, "network");
        Objects.requireNonNull(preference, "preference");
        Quality wanted = wanted(network, preference);
        if (has(track, wanted)) {
            return wanted;
        }
        Quality other = wanted == Quality.ORIGINAL ? Quality.AAC_256 : Quality.ORIGINAL;
        if (has(track, other)) {
            return other;
        }
        throw new IllegalArgumentException("la pista " + track.getId() + " no tiene ninguna versión que se pueda pedir");
    }

    private static Quality wanted(Network network, Preference preference) {
        switch (preference) {
            case ALWAYS_ORIGINAL:
                return Quality.ORIGINAL;
            case ALWAYS_AAC:
                return Quality.AAC_256;
            case AUTO:
                return network == Network.UNMETERED ? Quality.ORIGINAL : Quality.AAC_256;
            default:
                throw new IllegalStateException("preferencia sin tratar: " + preference);
        }
    }

    private static boolean has(Track track, Quality quality) {
        // El contrato manda siempre la lista, pero un JSON incompleto la deja en null.
        if (track.getFiles() == null) {
            return false;
        }
        for (TrackFile file : track.getFiles()) {
            if (file != null && quality.wire().equals(file.getQuality())) {
                return true;
            }
        }
        return false;
    }
}
