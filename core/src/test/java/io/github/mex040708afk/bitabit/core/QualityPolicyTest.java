package io.github.mex040708afk.bitabit.core;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertThrows;
import static org.junit.Assert.assertTrue;

import io.github.mex040708afk.bitabit.core.BitabitClient.Quality;
import io.github.mex040708afk.bitabit.core.Contract.Track;
import io.github.mex040708afk.bitabit.core.QualityPolicy.Network;
import io.github.mex040708afk.bitabit.core.QualityPolicy.Preference;
import org.junit.Test;

public class QualityPolicyTest {

    private static final String ORIG = "{\"quality\":\"orig\",\"codec\":\"flac\",\"lossless\":true,\"bits\":16,"
            + "\"sampleRate\":44100,\"channels\":2,\"bytes\":1000}";
    private static final String LOSSY_ORIG = "{\"quality\":\"orig\",\"codec\":\"mp3\",\"lossless\":false,"
            + "\"bits\":null,\"sampleRate\":44100,\"channels\":2,\"bytes\":400}";
    private static final String AAC = "{\"quality\":\"aac256\",\"codec\":\"aac\",\"lossless\":false,\"bits\":null,"
            + "\"sampleRate\":44100,\"channels\":2,\"bytes\":500}";

    /** Una pista del contrato con las versiones dadas, como JSON. */
    private static Track track(String files) {
        return Json.fromJson("{\"id\":9,\"title\":\"Uno\",\"artist\":\"Grupo\","
                + "\"album\":{\"id\":4,\"title\":\"Disco\"},\"albumArtist\":{\"id\":1,\"name\":\"Grupo\"},"
                + "\"discNumber\":null,\"trackNumber\":1,\"durationMs\":120000,\"files\":" + files + "}", Track.class);
    }

    private static final Track BOTH = track("[" + ORIG + "," + AAC + "]");
    private static final Track ONLY_LOSSY_ORIGINAL = track("[" + LOSSY_ORIG + "]");
    private static final Track ONLY_AAC = track("[" + AAC + "]");

    @Test
    public void autoPicksOriginalOnUnmetered() {
        assertEquals(Quality.ORIGINAL, QualityPolicy.choose(BOTH, Network.UNMETERED, Preference.AUTO));
    }

    @Test
    public void autoPicksAacOnMetered() {
        assertEquals(Quality.AAC_256, QualityPolicy.choose(BOTH, Network.METERED, Preference.AUTO));
    }

    @Test
    public void autoPicksAacOnUnknown() {
        assertEquals(Quality.AAC_256, QualityPolicy.choose(BOTH, Network.UNKNOWN, Preference.AUTO));
    }

    @Test
    public void alwaysOriginalIgnoresTheNetwork() {
        for (Network network : Network.values()) {
            assertEquals(Quality.ORIGINAL, QualityPolicy.choose(BOTH, network, Preference.ALWAYS_ORIGINAL));
        }
    }

    @Test
    public void alwaysAacIgnoresTheNetwork() {
        for (Network network : Network.values()) {
            assertEquals(Quality.AAC_256, QualityPolicy.choose(BOTH, network, Preference.ALWAYS_AAC));
        }
    }

    @Test
    public void lossyTrackWithoutAacFallsBackToTheOriginal() {
        for (Network network : Network.values()) {
            for (Preference preference : Preference.values()) {
                assertEquals(Quality.ORIGINAL, QualityPolicy.choose(ONLY_LOSSY_ORIGINAL, network, preference));
            }
        }
    }

    @Test
    public void trackWithOnlyAacFallsBackToIt() {
        assertEquals(Quality.AAC_256, QualityPolicy.choose(ONLY_AAC, Network.UNMETERED, Preference.ALWAYS_ORIGINAL));
        assertEquals(Quality.AAC_256, QualityPolicy.choose(ONLY_AAC, Network.UNMETERED, Preference.AUTO));
    }

    @Test
    public void filesOrderDoesNotMatter() {
        Track reversed = track("[" + AAC + "," + ORIG + "]");
        assertEquals(Quality.ORIGINAL, QualityPolicy.choose(reversed, Network.UNMETERED, Preference.AUTO));
        assertEquals(Quality.AAC_256, QualityPolicy.choose(reversed, Network.METERED, Preference.AUTO));
    }

    @Test
    public void unknownQualitiesAreIgnored() {
        Track odd = track("[{\"quality\":\"opus128\",\"codec\":\"opus\",\"lossless\":false,\"bits\":null,"
                + "\"sampleRate\":48000,\"channels\":2,\"bytes\":300}," + AAC + "]");
        assertEquals(Quality.AAC_256, QualityPolicy.choose(odd, Network.UNMETERED, Preference.AUTO));
    }

    @Test
    public void trackWithoutFilesFailsClearly() {
        IllegalArgumentException e = assertThrows(IllegalArgumentException.class,
                () -> QualityPolicy.choose(track("[]"), Network.UNMETERED, Preference.AUTO));
        assertTrue(e.getMessage().contains("9"));
    }

    @Test
    public void trackWithMissingFilesListFails() {
        Track noList = Json.fromJson("{\"id\":9,\"title\":\"Uno\"}", Track.class);
        assertThrows(IllegalArgumentException.class,
                () -> QualityPolicy.choose(noList, Network.METERED, Preference.AUTO));
    }

    @Test
    public void nullArgumentsAreRejected() {
        assertThrows(NullPointerException.class, () -> QualityPolicy.choose(null, Network.METERED, Preference.AUTO));
        assertThrows(NullPointerException.class, () -> QualityPolicy.choose(BOTH, null, Preference.AUTO));
        assertThrows(NullPointerException.class, () -> QualityPolicy.choose(BOTH, Network.METERED, null));
    }
}
