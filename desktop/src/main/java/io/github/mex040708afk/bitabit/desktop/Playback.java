package io.github.mex040708afk.bitabit.desktop;

import io.github.mex040708afk.bitabit.core.BitabitClient.Quality;
import io.github.mex040708afk.bitabit.core.Contract.Track;
import io.github.mex040708afk.bitabit.core.PlayQueue;
import io.github.mex040708afk.bitabit.core.QualityPolicy;
import io.github.mex040708afk.bitabit.core.QualityPolicy.Network;
import io.github.mex040708afk.bitabit.core.QualityPolicy.Preference;
import java.io.IOException;
import java.util.List;
import java.util.Optional;
import java.util.OptionalDouble;
import java.util.OptionalInt;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * La reproducción con cola (ADR-026): la {@link PlayQueue} de core, la versión que toca de
 * cada pista y el {@link Player}. Sin JavaFX, para poder probarlo.
 *
 * <p>Se usa solo desde el hilo de JavaFX, porque la cola no es segura entre hilos. Lo que
 * espera (pedir el enlace y hablar con mpv) va al {@code worker}, que hace las tareas de una
 * en una y en orden, y vuelve al hilo de JavaFX con el resultado.
 */
final class Playback {

    /** Lo que hace falta del {@link Player}: las pruebas lo cambian por uno de mentira. */
    interface Engine {

        void play(String url) throws IOException, InterruptedException;

        void pause() throws IOException, InterruptedException;

        void resume() throws IOException, InterruptedException;

        void togglePause() throws IOException, InterruptedException;

        void seek(double seconds) throws IOException, InterruptedException;

        void setExclusive(boolean exclusive) throws IOException, InterruptedException;

        void close() throws IOException;
    }

    /** Arranca el motor la primera vez que hace falta, o después de que muera. */
    @FunctionalInterface
    interface EngineStarter {
        Engine start(boolean exclusive) throws IOException, InterruptedException;
    }

    /** La URL firmada de una versión de una pista. Nunca se enseña: lleva la firma. */
    @FunctionalInterface
    interface LinkSource {
        String url(long trackId, Quality quality) throws IOException;
    }

    /** Lo que se cuenta a la interfaz, siempre en el hilo de JavaFX. */
    interface View {

        /** La pista actual de la cola, o vacío si no hay. */
        void nowPlaying(Optional<Track> track);

        /** Un problema que enseñar; vacío lo borra. */
        void problem(String message);

        /** El servidor ya no acepta el token. */
        void sessionExpired();
    }

    @FunctionalInterface
    private interface Command {
        void run(Engine engine) throws IOException, InterruptedException;
    }

    private final PlayQueue queue = new PlayQueue();
    private final Background worker;
    private final EngineStarter starter;
    private final LinkSource links;
    private final View view;

    /** Solo se toca desde las tareas del {@code worker}, que van de una en una en el mismo hilo. */
    private Engine engine;
    /** La última carga pedida: una tarea de carga que ya no es la última no hace nada. */
    private final AtomicInteger latestLoad = new AtomicInteger();

    private boolean exclusive = true;
    private long positionMs;
    /** Cargas pedidas que aún no han terminado: mientras haya, un EOF es de la pista que se va. */
    private int loading;
    private boolean closed;

    Playback(Background worker, EngineStarter starter, LinkSource links, View view) {
        this.worker = worker;
        this.starter = starter;
        this.links = links;
        this.view = view;
    }

    /** Cambia la cola por {@code tracks} y empieza por la de {@code index}. */
    void play(List<Track> tracks, int index) {
        if (closed) {
            return;
        }
        queue.setTracks(tracks, index);
        queue.current().ifPresent(this::load);
    }

    void next() {
        if (!closed) {
            queue.next().ifPresent(this::load);
        }
    }

    /** Pasados {@link PlayQueue#RESTART_THRESHOLD_MS}, o en la primera, vuelve a empezar la pista. */
    void previous() {
        if (closed) {
            return;
        }
        OptionalInt before = queue.currentIndex();
        Optional<Track> track = queue.previous(positionMs);
        if (track.isEmpty()) {
            return;
        }
        if (queue.currentIndex().equals(before) && loading == 0) {
            send("volver al principio de la pista", engine -> engine.seek(0));
        } else {
            load(track.get());
        }
    }

    void togglePause() {
        if (queue.current().isPresent()) {
            send("pausar o reanudar", Engine::togglePause);
        }
    }

    void pause() {
        if (queue.current().isPresent()) {
            send("pausar", Engine::pause);
        }
    }

    void seek(double seconds) {
        if (queue.current().isPresent() && Double.isFinite(seconds) && seconds >= 0) {
            send("saltar", engine -> engine.seek(seconds));
        }
    }

    /** Vale para el mpv que ya suena y para el que se arranque después. */
    void setExclusive(boolean exclusive) {
        this.exclusive = exclusive;
        send("cambiar el modo exclusivo", engine -> engine.setExclusive(exclusive));
    }

    boolean exclusive() {
        return exclusive;
    }

    Optional<Track> current() {
        return queue.current();
    }

    /** Lo que avisa el Player, ya en el hilo de JavaFX. */
    void onPosition(OptionalDouble seconds) {
        positionMs = seconds.isPresent() ? (long) (seconds.getAsDouble() * 1000) : 0;
    }

    void onTrackEnded(EndReason reason, Optional<String> error) {
        if (closed) {
            return;
        }
        switch (reason) {
            case EOF -> {
                if (loading == 0) {
                    queue.onTrackEnded().ifPresent(this::load);
                }
            }
            case ERROR -> view.problem("No se pudo reproducir «"
                    + queue.current().map(t -> Display.name(t.getTitle())).orElse("la pista") + "»"
                    + error.map(e -> ": " + e).orElse("") + "."
                    + (exclusive ? " Si otra app tiene la tarjeta en modo exclusivo, desmarca «Modo exclusivo»." : ""));
            // STOP llega al sustituir una pista; los demás no piden hacer nada.
            default -> {
            }
        }
    }

    /** mpv murió: el siguiente comando arranca otro. */
    void onEngineFailed(IOException error) {
        if (closed) {
            return;
        }
        view.problem("mpv se ha cerrado: " + error.getMessage());
        worker.run(() -> {
            Engine dead = engine;
            engine = null;
            if (dead != null) {
                dead.close();
            }
            return null;
        }, ok -> {
        }, e -> view.problem("mpv se ha cerrado y no se pudo cerrar del todo: " + e.getMessage()));
    }

    /** Cierra mpv. Después no hace nada más. */
    void close() {
        if (closed) {
            return;
        }
        closed = true;
        latestLoad.incrementAndGet();
        worker.run(() -> {
            Engine open = engine;
            engine = null;
            if (open != null) {
                open.close();
            }
            return null;
        }, ok -> {
        }, e -> view.problem("No se pudo cerrar mpv: " + e.getMessage()));
    }

    private void load(Track track) {
        view.nowPlaying(Optional.of(track));
        view.problem("");
        positionMs = 0;
        loading++;
        int mine = latestLoad.incrementAndGet();
        boolean exclusiveNow = exclusive;
        worker.run(() -> {
            if (mine != latestLoad.get()) {
                // Ya se ha pedido otra: no hace falta ni pedir el enlace de esta.
                return null;
            }
            Quality quality = QualityPolicy.choose(track, Network.UNMETERED, Preference.AUTO);
            String url = links.url(track.getId(), quality);
            Engine running = engine(exclusiveNow);
            try {
                // mpv guarda la pausa entre pistas: una pista nueva empieza sonando.
                running.resume();
                running.play(url);
            } catch (IOException e) {
                throw engineError(e);
            }
            return null;
        }, ok -> loading--, error -> {
            loading--;
            if (error instanceof Catalog.SessionExpired) {
                close();
                view.sessionExpired();
                return;
            }
            view.problem(error instanceof IOException ? error.getMessage()
                    : error instanceof IllegalArgumentException ? "No se puede reproducir: " + error.getMessage()
                    : "Error inesperado al reproducir: " + error);
        });
    }

    private Engine engine(boolean exclusiveNow) throws IOException, InterruptedException {
        if (engine == null) {
            engine = starter.start(exclusiveNow);
        }
        return engine;
    }

    /** Un comando para el mpv que ya suena; sin mpv no hay nada que mandar. */
    private void send(String what, Command command) {
        if (closed) {
            return;
        }
        worker.run(() -> {
            if (engine != null) {
                try {
                    command.run(engine);
                } catch (IOException e) {
                    throw engineError(e);
                }
            }
            return null;
        }, ok -> {
        }, error -> view.problem(error instanceof IOException ? error.getMessage()
                : "Error inesperado al " + what + ": " + error));
    }

    /**
     * El Player dice «no se pudo pausar» y deja el porqué en la causa: se juntan los dos
     * para enseñarlo. Ninguno lleva el enlace.
     */
    private static IOException engineError(IOException e) {
        String what = e.getMessage() == null ? "error de mpv" : e.getMessage();
        String why = e.getCause() == null || e.getCause().getMessage() == null ? "" : ": " + e.getCause().getMessage();
        return new IOException(Character.toUpperCase(what.charAt(0)) + what.substring(1) + why + ".", e);
    }
}
