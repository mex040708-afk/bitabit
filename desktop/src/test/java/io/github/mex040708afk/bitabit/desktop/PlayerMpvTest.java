package io.github.mex040708afk.bitabit.desktop;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;
import static org.junit.Assume.assumeTrue;

import java.io.File;
import java.io.IOException;
import java.nio.file.Path;
import java.time.Duration;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.Predicate;
import org.junit.After;
import org.junit.Before;
import org.junit.BeforeClass;
import org.junit.ClassRule;
import org.junit.Test;
import org.junit.rules.TemporaryFolder;

/**
 * Reproduce con un mpv de verdad un tono que FFmpeg genera en el momento: en
 * Git no hay audio. Con {@code --ao=null}, para que no suene la tarjeta. Solo
 * corre si BITABIT_MPV dice dónde está mpv y FFmpeg está en el PATH.
 */
public class PlayerMpvTest {

    private static final Duration WAIT = Duration.ofSeconds(10);

    @ClassRule
    public static final TemporaryFolder FOLDER = new TemporaryFolder();

    private static String tone;

    private final RecordingListener listener = new RecordingListener();
    private Player player;

    @BeforeClass
    public static void generateTone() throws Exception {
        assumeTrue("sin BITABIT_MPV", System.getenv("BITABIT_MPV") != null);
        Path file = FOLDER.getRoot().toPath().resolve("tono.flac");
        Process ffmpeg;
        try {
            ffmpeg = new ProcessBuilder("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
                    "-f", "lavfi", "-i", "sine=frequency=440:duration=3", "-c:a", "flac", file.toString())
                    .redirectOutput(ProcessBuilder.Redirect.DISCARD)
                    .redirectError(ProcessBuilder.Redirect.DISCARD)
                    .start();
        } catch (IOException e) {
            assumeTrue("sin ffmpeg en el PATH: " + e.getMessage(), false);
            return;
        }
        if (!ffmpeg.waitFor(30, TimeUnit.SECONDS)) {
            ffmpeg.destroyForcibly();
            fail("ffmpeg no generó el tono en 30 s");
        }
        assertEquals("ffmpeg falló", 0, ffmpeg.exitValue());
        tone = file.toString();
    }

    @Before
    public void startPlayer() throws Exception {
        player = Player.start(System.getenv("BITABIT_MPV"), "null", false, listener);
    }

    @After
    public void closePlayer() throws IOException {
        if (player != null) {
            player.close();
        }
    }

    @Test
    public void playsPausesSeeksAndEndsNaturally() throws Exception {
        player.play(tone);
        listener.await(c -> c.startsWith("duration ") && !c.equals("duration -"), WAIT);
        assertEquals(3.0, listener.duration().getAsDouble(), 0.1);
        listener.await(c -> c.equals("decoded s16, 44100 Hz, 1 canal"), WAIT);
        listener.await(positionAtLeast(0.3), WAIT);

        player.pause();
        listener.await("pause true"::equals, WAIT);
        double paused = listener.position().getAsDouble();
        Thread.sleep(500);
        assertEquals("en pausa no avanza", paused, listener.position().getAsDouble(), 0.05);

        player.resume();
        listener.await("pause false"::equals, WAIT);
        player.togglePause();
        listener.await("pause true"::equals, WAIT);
        listener.clear();
        player.togglePause();
        listener.await("pause false"::equals, WAIT);

        listener.clear();
        player.seek(2.5);
        listener.await(positionAtLeast(2.4), WAIT);

        String end = listener.await(c -> c.startsWith("ended "), WAIT);
        assertEquals("ended EOF", end);
        assertFalse(listener.failure().isPresent());
    }

    @Test
    public void replacingTheTrackStopsThePreviousOne() throws Exception {
        player.play(tone);
        listener.await(c -> c.startsWith("position ") && !c.equals("position -"), WAIT);
        player.play(tone);
        assertEquals("ended STOP", listener.await(c -> c.startsWith("ended "), WAIT));
        listener.clear();
        player.stop();
        assertEquals("ended STOP", listener.await(c -> c.startsWith("ended "), WAIT));
    }

    @Test
    public void reportsTracksThatCannotBeOpened() throws Exception {
        player.play(new File(FOLDER.getRoot(), "no-existe.flac").getPath());
        String end = listener.await(c -> c.startsWith("ended "), WAIT);
        assertTrue(end, end.startsWith("ended ERROR "));
    }

    @Test
    public void changesExclusiveModeWithoutRestarting() throws Exception {
        player.play(tone);
        listener.await(c -> c.startsWith("position ") && !c.equals("position -"), WAIT);
        long pid = player.mpv.process().pid();
        player.setExclusive(true);
        assertTrue(player.exclusive());
        assertTrue(player.mpv.command("get_property", "audio-exclusive").getAsBoolean());
        assertEquals(pid, player.mpv.process().pid());
        player.setExclusive(false);
        assertFalse(player.mpv.command("get_property", "audio-exclusive").getAsBoolean());
    }

    @Test
    public void keepsVolumeAndReplayGainUntouched() throws Exception {
        assertEquals(100.0, player.mpv.command("get_property", "volume").getAsDouble(), 0);
        // mpv devuelve «no» como false.
        assertFalse(player.mpv.command("get_property", "replaygain").getAsBoolean());
    }

    @Test
    public void reportsMpvDying() throws Exception {
        player.mpv.process().destroyForcibly();
        listener.await("failure"::equals, WAIT);
        assertTrue(listener.failure().orElseThrow().getMessage().contains("mpv"));
    }

    @Test
    public void errorsDoNotCarryTheSignedLink() throws Exception {
        String link = "http://127.0.0.1:9/media/42.flac?exp=1700000000&sig=firma-secreta";
        player.close();
        try {
            player.play(link);
            fail("con mpv cerrado no se puede cargar nada");
        } catch (IOException e) {
            assertTrue(e.getMessage(), e.getMessage().contains("/media/42.flac"));
            for (Throwable t = e; t != null; t = t.getCause()) {
                assertFalse(String.valueOf(t), String.valueOf(t).contains("firma-secreta"));
                for (Throwable s : t.getSuppressed()) {
                    assertFalse(String.valueOf(s), String.valueOf(s).contains("firma-secreta"));
                }
            }
        }
        assertFalse(player.toString().contains("firma-secreta"));
        assertFalse(listener.calls().stream().anyMatch(c -> c.contains("firma-secreta")));
    }

    @Test
    public void listenerCannotSendCommands() throws Exception {
        AtomicReference<Player> self = new AtomicReference<>();
        AtomicReference<Throwable> thrown = new AtomicReference<>();
        RecordingListener calling = new RecordingListener() {
            @Override
            public void onPause(boolean paused) {
                Player p = self.get();
                if (p != null && paused) {
                    try {
                        p.stop();
                    } catch (IOException | InterruptedException | RuntimeException e) {
                        thrown.set(e);
                    }
                }
                super.onPause(paused);
            }
        };
        try (Player p = Player.start(System.getenv("BITABIT_MPV"), "null", false, calling)) {
            self.set(p);
            long started = System.nanoTime();
            p.pause();
            calling.await("pause true"::equals, WAIT);
            assertTrue(String.valueOf(thrown.get()), thrown.get() instanceof IllegalStateException);
            assertTrue("no esperó la respuesta", System.nanoTime() - started < Duration.ofSeconds(2).toNanos());
            assertFalse(calling.failure().isPresent());
        }
    }

    private static Predicate<String> positionAtLeast(double seconds) {
        return c -> c.startsWith("position ") && !c.equals("position -")
                && Double.parseDouble(c.substring("position ".length())) >= seconds;
    }
}
