package io.github.mex040708afk.bitabit.desktop;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertThrows;
import static org.junit.Assert.assertTrue;

import io.github.mex040708afk.bitabit.core.BitabitClient;
import io.github.mex040708afk.bitabit.core.BitabitClient.Quality;
import io.github.mex040708afk.bitabit.core.Contract.Track;
import io.github.mex040708afk.bitabit.core.Json;
import java.io.IOException;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Deque;
import java.util.List;
import java.util.Optional;
import java.util.OptionalDouble;
import java.util.concurrent.Callable;
import java.util.function.Consumer;
import org.junit.Before;
import org.junit.Test;

/** La reproducción con cola, sin mpv ni servidor: un motor y unos enlaces de mentira. */
public class PlaybackTest {

    /** Guarda las tareas y las hace cuando la prueba quiere, como el hilo «reproduccion». */
    private static final class Worker implements Background {

        private final Deque<Runnable> tasks = new ArrayDeque<>();

        @Override
        public <T> void run(Callable<T> task, Consumer<T> done, Consumer<Throwable> failed) {
            tasks.add(() -> {
                T result;
                try {
                    result = task.call();
                } catch (Exception e) {
                    failed.accept(e);
                    return;
                }
                done.accept(result);
            });
        }

        void runAll() {
            while (!tasks.isEmpty()) {
                tasks.poll().run();
            }
        }
    }

    private static final class Engine implements Playback.Engine {

        final List<String> calls = new ArrayList<>();
        final boolean exclusive;
        IOException failNext;

        Engine(boolean exclusive) {
            this.exclusive = exclusive;
        }

        private void call(String what) throws IOException {
            calls.add(what);
            if (failNext != null) {
                IOException e = failNext;
                failNext = null;
                throw e;
            }
        }

        @Override
        public void play(String url) throws IOException {
            call("play " + url);
        }

        @Override
        public void pause() throws IOException {
            call("pause");
        }

        @Override
        public void resume() throws IOException {
            call("resume");
        }

        @Override
        public void togglePause() throws IOException {
            call("toggle");
        }

        @Override
        public void seek(double seconds) throws IOException {
            call("seek " + seconds);
        }

        @Override
        public void setExclusive(boolean exclusive) throws IOException {
            call("exclusive " + exclusive);
        }

        @Override
        public void close() throws IOException {
            call("close");
        }
    }

    private static final class View implements Playback.View {

        final List<Optional<Track>> playing = new ArrayList<>();
        String problem = "";
        int expired;

        @Override
        public void nowPlaying(Optional<Track> track) {
            playing.add(track);
        }

        @Override
        public void problem(String message) {
            problem = message;
        }

        @Override
        public void sessionExpired() {
            expired++;
        }
    }

    private static final String BOTH = "[{\"quality\":\"orig\",\"codec\":\"flac\",\"lossless\":true,\"sampleRate\":48000},"
            + "{\"quality\":\"aac256\",\"codec\":\"aac\",\"lossless\":false,\"sampleRate\":48000}]";
    private static final String AAC_ONLY = "[{\"quality\":\"aac256\",\"codec\":\"aac\",\"lossless\":false,\"sampleRate\":44100}]";

    private final Worker worker = new Worker();
    private final View view = new View();
    private final List<Engine> engines = new ArrayList<>();
    private final List<String> links = new ArrayList<>();
    private IOException linkError;
    private Playback playback;
    private List<Track> album;

    @Before
    public void setUp() {
        playback = new Playback(worker, exclusive -> {
            Engine engine = new Engine(exclusive);
            engines.add(engine);
            return engine;
        }, (id, quality) -> {
            if (linkError != null) {
                throw linkError;
            }
            links.add(id + " " + quality);
            return "http://127.0.0.1:8080/media/" + id + (quality == Quality.ORIGINAL ? ".flac" : ".m4a") + "?f=1";
        }, view);
        album = List.of(track(1, BOTH), track(2, BOTH), track(3, AAC_ONLY));
    }

    @Test
    public void playsFromTheChosenTrackWithTheOriginal() {
        playback.play(album, 1);
        worker.runAll();

        assertEquals(List.of("2 ORIGINAL"), links);
        assertEquals(1, engines.size());
        assertTrue("Purista por defecto", engines.get(0).exclusive);
        assertEquals(List.of("resume", "play http://127.0.0.1:8080/media/2.flac?f=1"), engines.get(0).calls);
        assertEquals(2, playback.current().orElseThrow().getId());
        assertEquals(Optional.of(album.get(1)), view.playing.get(view.playing.size() - 1));
    }

    @Test
    public void aTrackWithoutOriginalPlaysItsAac() {
        playback.play(album, 2);
        worker.runAll();

        assertEquals(List.of("3 AAC_256"), links);
    }

    @Test
    public void theEndOfATrackPlaysTheNextUntilTheQueueEnds() {
        playback.play(album, 1);
        worker.runAll();
        playback.onTrackEnded(EndReason.EOF, Optional.empty());
        worker.runAll();
        playback.onTrackEnded(EndReason.EOF, Optional.empty());
        worker.runAll();

        assertEquals(List.of("2 ORIGINAL", "3 AAC_256"), links);
        assertEquals(3, playback.current().orElseThrow().getId());
        assertEquals("un solo mpv para toda la cola", 1, engines.size());
    }

    @Test
    public void anEndWhileTheUserLoadsAnotherDoesNotSkipOne() {
        playback.play(album, 0);
        worker.runAll();
        playback.next();
        // La pista 1 acaba sola justo cuando se carga la 2: no tiene que saltar a la 3.
        playback.onTrackEnded(EndReason.EOF, Optional.empty());
        worker.runAll();

        assertEquals(List.of("1 ORIGINAL", "2 ORIGINAL"), links);
    }

    @Test
    public void replacingATrackIsNotAnEnd() {
        playback.play(album, 0);
        worker.runAll();
        playback.onTrackEnded(EndReason.STOP, Optional.empty());
        worker.runAll();

        assertEquals(List.of("1 ORIGINAL"), links);
    }

    @Test
    public void anErrorStopsTheQueueAndSaysWhy() {
        playback.play(album, 0);
        worker.runAll();
        playback.onTrackEnded(EndReason.ERROR, Optional.of("audio output initialization failed"));
        worker.runAll();

        assertEquals(List.of("1 ORIGINAL"), links);
        assertTrue(view.problem, view.problem.startsWith("No se pudo reproducir «Pista 1»: audio output"));
        assertTrue(view.problem, view.problem.contains("Modo exclusivo"));
    }

    @Test
    public void quickNextsOnlyLoadTheLast() {
        playback.play(album, 0);
        playback.next();
        playback.next();
        worker.runAll();

        assertEquals(List.of("3 AAC_256"), links);
        assertEquals(List.of("resume", "play http://127.0.0.1:8080/media/3.m4a?f=1"), engines.get(0).calls);
    }

    @Test
    public void previousRestartsAfterThreeSecondsAndGoesBackBefore() {
        playback.play(album, 1);
        worker.runAll();
        playback.onPosition(OptionalDouble.of(5));
        playback.previous();
        worker.runAll();
        assertEquals("seek 0.0", last(engines.get(0).calls));
        assertEquals(List.of("2 ORIGINAL"), links);

        playback.onPosition(OptionalDouble.of(1));
        playback.previous();
        worker.runAll();
        assertEquals(List.of("2 ORIGINAL", "1 ORIGINAL"), links);
    }

    @Test
    public void pauseAndSeekGoToMpv() {
        playback.togglePause();
        worker.runAll();
        assertTrue("sin cola no se arranca mpv", engines.isEmpty());

        playback.play(album, 0);
        playback.togglePause();
        playback.seek(42.5);
        playback.seek(Double.NaN);
        worker.runAll();
        assertEquals(List.of("resume", "play http://127.0.0.1:8080/media/1.flac?f=1", "toggle", "seek 42.5"),
                engines.get(0).calls);
    }

    @Test
    public void theExclusiveModeIsKeptForTheNextMpv() {
        playback.setExclusive(false);
        worker.runAll();
        playback.play(album, 0);
        worker.runAll();

        assertEquals(1, engines.size());
        assertTrue(!engines.get(0).exclusive);
        assertTrue(!playback.exclusive());
    }

    @Test
    public void anExpiredSessionStopsEverything() throws IOException {
        try (FakeServer server = new FakeServer()) {
            server.reply("GET /api/v1/tracks/1/link", FakeServer.Reply.json(401,
                    "{\"error\":\"unauthorized\",\"message\":\"token no válido\"}"));
            BitabitClient client = new BitabitClient(server.address());
            client.useToken(SessionStoreTest.TOKEN);
            linkError = assertThrows(Catalog.SessionExpired.class,
                    () -> new Catalog(client, server.address()).link(1, Quality.ORIGINAL));
        }
        playback.play(album, 0);
        worker.runAll();

        assertEquals(1, view.expired);
        assertTrue(engines.isEmpty());
        playback.next();
        worker.runAll();
        assertTrue("cerrada, no hace nada más", engines.isEmpty());
    }

    @Test
    public void aLinkErrorIsShownWithItsContext() {
        linkError = new IOException("No se pudo cargar el enlace de la pista. El servidor respondió 500.");
        playback.play(album, 0);
        worker.runAll();

        assertEquals("No se pudo cargar el enlace de la pista. El servidor respondió 500.", view.problem);
        assertEquals(0, view.expired);
    }

    @Test
    public void anMpvErrorKeepsTheReasonButNotTheLink() {
        playback.play(album, 0);
        worker.runAll();
        engines.get(0).failNext = new IOException("no se pudo pausar", new IOException("tubería rota"));
        playback.togglePause();
        worker.runAll();

        assertEquals("No se pudo pausar: tubería rota.", view.problem);
    }

    @Test
    public void whenMpvDiesTheNextTrackStartsAnother() {
        playback.play(album, 0);
        worker.runAll();
        playback.onEngineFailed(new IOException("mpv terminó con el código 1"));
        worker.runAll();
        assertEquals("close", last(engines.get(0).calls));
        assertTrue(view.problem, view.problem.startsWith("mpv se ha cerrado"));

        playback.next();
        worker.runAll();
        assertEquals(2, engines.size());
    }

    @Test
    public void closingClosesMpvOnce() {
        playback.play(album, 0);
        worker.runAll();
        playback.close();
        playback.close();
        playback.play(album, 1);
        playback.onTrackEnded(EndReason.EOF, Optional.empty());
        worker.runAll();

        assertEquals(List.of("resume", "play http://127.0.0.1:8080/media/1.flac?f=1", "close"), engines.get(0).calls);
        assertEquals(List.of("1 ORIGINAL"), links);
    }

    private static String last(List<String> list) {
        return list.get(list.size() - 1);
    }

    private static Track track(long id, String files) {
        return Json.fromJson("{\"id\":" + id + ",\"title\":\"Pista " + id + "\",\"artist\":\"A\","
                + "\"durationMs\":60000,\"files\":" + files + "}", Track.class);
    }
}
