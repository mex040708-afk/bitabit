package io.github.mex040708afk.bitabit.desktop;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;

import com.google.gson.JsonNull;
import com.google.gson.JsonParser;
import org.junit.Test;

public class AudioParamsTest {

    @Test
    public void readsWhatMpvSends() {
        String data = """
                {"format":"s32","samplerate":96000,"channels":"stereo","hr-channels":"stereo","channel-count":2}""";
        assertEquals(new AudioParams("s32", 96_000, 2), AudioParams.from(JsonParser.parseString(data)).orElseThrow());
    }

    // Mientras no hay audio, el evento llega sin «data» o con un objeto a medias.
    @Test
    public void emptyWithoutAudio() {
        assertTrue(AudioParams.from(null).isEmpty());
        assertTrue(AudioParams.from(JsonNull.INSTANCE).isEmpty());
        assertTrue(AudioParams.from(JsonParser.parseString("{\"format\":\"s16\"}")).isEmpty());
    }
}
