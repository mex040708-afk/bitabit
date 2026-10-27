package io.github.mex040708afk.bitabit.desktop;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import io.github.mex040708afk.bitabit.core.Contract.Album;
import io.github.mex040708afk.bitabit.core.Json;
import java.util.List;
import org.junit.Test;

public class DisplayTest {

    private static final String FLAC_24_96 = "{\"quality\":\"orig\",\"codec\":\"flac\",\"lossless\":true,"
            + "\"bits\":24,\"sampleRate\":96000,\"channels\":2,\"bytes\":1}";
    private static final String MP3 = "{\"quality\":\"orig\",\"codec\":\"mp3\",\"lossless\":false,"
            + "\"bits\":null,\"sampleRate\":44100,\"channels\":2,\"bytes\":1}";

    @Test
    public void durationUnderAnHourIsMinutesAndSeconds() {
        assertEquals("0:00", Display.duration(0));
        assertEquals("0:59", Display.duration(59_999));
        assertEquals("3:35", Display.duration(215_999));
        assertEquals("59:59", Display.duration(3_599_000));
    }

    @Test
    public void durationFromAnHourHasHours() {
        assertEquals("1:00:00", Display.duration(3_600_000));
        assertEquals("2:03:09", Display.duration(7_389_000));
    }

    @Test
    public void negativeDurationIsADash() {
        assertEquals("–", Display.duration(-1));
    }

    @Test
    public void blankNamesGetTheFiller() {
        assertEquals(Display.UNNAMED, Display.name(""));
        assertEquals(Display.UNNAMED, Display.name("   "));
        assertEquals(Display.UNNAMED, Display.name(null));
        assertTrue(Display.isBlank(""));
        assertFalse(Display.isBlank("Kind of Blue"));
        assertEquals("Kind of Blue", Display.name(" Kind of Blue "));
    }

    @Test
    public void markupInNamesStaysText() {
        assertEquals("<b>x</b>", Display.name("<b>x</b>"));
    }

    @Test
    public void countUsesSingularAndPlural() {
        assertEquals("1 álbum", Display.count(1, "álbum", "álbumes"));
        assertEquals("0 álbumes", Display.count(0, "álbum", "álbumes"));
        assertEquals("2 álbumes", Display.count(2, "álbum", "álbumes"));
    }

    @Test
    public void formatOfLosslessHasBitsAndRate() {
        assertEquals("FLAC 24/96", Display.format(album(track(1, 1, FLAC_24_96)).getTracks().get(0).getFiles()));
    }

    @Test
    public void formatOfLossyHasRateInKilohertz() {
        assertEquals("MP3 44.1 kHz", Display.format(album(track(1, 1, MP3)).getTracks().get(0).getFiles()));
    }

    @Test
    public void formatWithoutFilesIsEmpty() {
        assertEquals("", Display.format(List.of()));
        assertEquals("", Display.format(null));
    }

    @Test
    public void rowsOfASingleDiscShowTheTrackNumber() {
        Album album = album(track(1, 3, FLAC_24_96));
        assertEquals(new Display.TrackRow("3", "Pista 1", "3:35", "FLAC 24/96"), Display.rows(album).get(0));
    }

    @Test
    public void rowsOfSeveralDiscsShowTheDisc() {
        Album album = album(track(1, 5, FLAC_24_96), track(2, 1, FLAC_24_96));
        assertEquals("1-05", Display.rows(album).get(0).number());
        assertEquals("2-01", Display.rows(album).get(1).number());
    }

    @Test
    public void rowsWithoutNumberOrTitleStillPaint() {
        Album album = Json.fromJson("{\"id\":1,\"title\":\"\",\"tracks\":[{\"id\":9,\"title\":\"\","
                + "\"durationMs\":1000,\"files\":[]}]}", Album.class);
        assertEquals(new Display.TrackRow("–", Display.UNNAMED, "0:01", ""), Display.rows(album).get(0));
    }

    private static Album album(String... tracks) {
        return Json.fromJson("{\"id\":1,\"title\":\"A\",\"tracks\":[" + String.join(",", tracks) + "]}", Album.class);
    }

    private static String track(int disc, int number, String file) {
        return "{\"id\":" + (disc * 100 + number) + ",\"title\":\"Pista " + disc + "\",\"discNumber\":" + disc
                + ",\"trackNumber\":" + number + ",\"durationMs\":215999,\"files\":[" + file + "]}";
    }
}
