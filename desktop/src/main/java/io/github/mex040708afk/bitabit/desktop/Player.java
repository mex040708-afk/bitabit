package io.github.mex040708afk.bitabit.desktop;

import java.io.IOException;
import java.net.URI;
import java.net.URISyntaxException;
import java.util.List;
import java.util.Optional;
import java.util.OptionalDouble;

/**
 * Reproduce pistas con un único mpv, vivo mientras viva el Player (ADR-020).
 * No conoce la cola: quien la lleve llama a {@link #play} con la siguiente
 * cuando llega {@link Listener#onTrackEnded} con {@link EndReason#EOF}.
 *
 * <p>El audio es cosa de mpv (ADR-004): volumen a 100, sin ReplayGain ni
 * remuestreo, con las opciones de siempre de {@link Mpv}.
 *
 * <p>Los comandos esperan la respuesta de mpv, como mucho 5 s, y se pueden
 * llamar desde cualquier hilo salvo desde el {@link Listener}. El enlace de la
 * pista nunca sale en un mensaje ni en una excepción: lleva la firma.
 */
final class Player implements AutoCloseable, Playback.Engine {

    /**
     * El estado de la reproducción. Todo llega desde el hilo que lee la
     * tubería de mpv («mpv-ipc»): la interfaz tiene que pasarlo a su hilo
     * ({@code Platform.runLater}) y no puede llamar al Player desde aquí, porque
     * ese hilo es el que tendría que leer la respuesta.
     */
    interface Listener {

        /** La posición en segundos. Vacía sin pista. mpv la manda muchas veces por segundo. */
        void onPosition(OptionalDouble seconds);

        /** La duración en segundos. Vacía sin pista o si mpv no la sabe. */
        void onDuration(OptionalDouble seconds);

        void onPause(boolean paused);

        /** El formato que da el decodificador. Vacío sin pista. */
        void onDecodedFormat(Optional<AudioParams> params);

        /** El formato que mpv manda a Windows. Vacío sin pista. */
        void onOutputFormat(Optional<AudioParams> params);

        /**
         * Terminó la pista. Al sustituirla con {@link #play} llega {@link EndReason#STOP}
         * de la anterior, que no es un fin natural.
         *
         * @param error el error de mpv, si lo hubo (sin el enlace)
         */
        void onTrackEnded(EndReason reason, Optional<String> error);

        /** mpv murió o cortó la tubería sin que se cerrara el Player. Ya no se puede usar. */
        void onFailure(IOException error);
    }

    /** Visible en el paquete para que las pruebas puedan matar mpv. */
    final Mpv mpv;
    private volatile boolean exclusive;

    private Player(Mpv mpv, boolean exclusive) {
        this.mpv = mpv;
        this.exclusive = exclusive;
    }

    /**
     * Arranca mpv con salida WASAPI.
     *
     * @param exe la ruta de mpv.exe, o «mpv» para buscarlo en el PATH
     * @param exclusive modo exclusivo de WASAPI (Purista)
     */
    static Player start(String exe, boolean exclusive, Listener listener) throws IOException, InterruptedException {
        return start(exe, "wasapi", exclusive, listener);
    }

    /** Con otra salida de audio: las pruebas usan {@code null} para que no suene nada. */
    static Player start(String exe, String audioOutput, boolean exclusive, Listener listener)
            throws IOException, InterruptedException {
        Mpv mpv = Mpv.start(exe, List.of("--ao=" + audioOutput, "--audio-exclusive=" + flag(exclusive)),
                new PlayerEvents(listener));
        try {
            for (int i = 0; i < PlayerEvents.PROPERTIES.size(); i++) {
                mpv.observe(i + 1, PlayerEvents.PROPERTIES.get(i));
            }
        } catch (IOException | InterruptedException | RuntimeException e) {
            try {
                mpv.close();
            } catch (IOException closing) {
                e.addSuppressed(closing);
            }
            throw e;
        }
        return new Player(mpv, exclusive);
    }

    /** Carga la pista en lugar de la que suene y la reproduce. */
    public void play(String url) throws IOException, InterruptedException {
        if (url == null || url.isBlank()) {
            throw new IllegalArgumentException("la pista no tiene enlace");
        }
        // Por la tubería, para que el enlace no salga en la lista de procesos.
        command("cargar " + describe(url), "loadfile", url, "replace");
    }

    public void pause() throws IOException, InterruptedException {
        command("pausar", "set", "pause", "yes");
    }

    public void resume() throws IOException, InterruptedException {
        command("reanudar", "set", "pause", "no");
    }

    public void togglePause() throws IOException, InterruptedException {
        command("pausar o reanudar", "cycle", "pause");
    }

    /** Salta a ese segundo de la pista. */
    public void seek(double seconds) throws IOException, InterruptedException {
        if (!Double.isFinite(seconds) || seconds < 0) {
            throw new IllegalArgumentException("posición no válida: " + seconds);
        }
        command("saltar a " + seconds + " s", "seek", seconds, "absolute");
    }

    /** Para y descarga la pista; mpv sigue vivo, esperando la siguiente. */
    void stop() throws IOException, InterruptedException {
        command("parar", "stop");
    }

    /**
     * Cambia el modo exclusivo sin reiniciar mpv: al cambiar la opción, mpv
     * vuelve a abrir la salida de audio y la pista sigue donde estaba.
     */
    public void setExclusive(boolean exclusive) throws IOException, InterruptedException {
        command("cambiar el modo exclusivo", "set", "audio-exclusive", flag(exclusive));
        this.exclusive = exclusive;
    }

    boolean exclusive() {
        return exclusive;
    }

    /** Cierra mpv; no llegan más avisos. */
    @Override
    public void close() throws IOException {
        mpv.close();
    }

    /** Lo que se puede contar de un enlace: la ruta {@code /media/{id}.{ext}}, sin la firma. */
    static String describe(String url) {
        URI uri;
        try {
            uri = new URI(url);
        } catch (URISyntaxException e) {
            return "la pista";
        }
        String scheme = uri.getScheme();
        if (("http".equals(scheme) || "https".equals(scheme)) && uri.getPath() != null && !uri.getPath().isEmpty()) {
            return Links.safe(uri);
        }
        return "la pista";
    }

    private void command(String what, Object... args) throws IOException, InterruptedException {
        try {
            mpv.command(args);
        } catch (IOException e) {
            throw new IOException("no se pudo " + what, e);
        }
    }

    private static String flag(boolean value) {
        return value ? "yes" : "no";
    }
}
