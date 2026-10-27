package io.github.mex040708afk.bitabit.desktop;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;

import com.google.gson.JsonParser;
import java.io.IOException;
import java.util.List;
import java.util.Optional;
import org.junit.Test;

public class PlayerEventsTest {

    private final RecordingListener listener = new RecordingListener();
    private final PlayerEvents events = new PlayerEvents(listener);

    private void receive(String json) {
        events.onEvent(JsonParser.parseString(json).getAsJsonObject());
    }

    @Test
    public void translatesProperties() {
        receive("{\"event\":\"property-change\",\"id\":1,\"name\":\"time-pos\",\"data\":1.5}");
        receive("{\"event\":\"property-change\",\"id\":2,\"name\":\"duration\",\"data\":180.25}");
        receive("{\"event\":\"property-change\",\"id\":3,\"name\":\"pause\",\"data\":true}");
        receive("{\"event\":\"property-change\",\"id\":4,\"name\":\"audio-params\","
                + "\"data\":{\"format\":\"s32\",\"samplerate\":96000,\"channels\":\"stereo\",\"channel-count\":2}}");
        receive("{\"event\":\"property-change\",\"id\":5,\"name\":\"audio-out-params\","
                + "\"data\":{\"format\":\"float\",\"samplerate\":48000,\"channels\":\"stereo\",\"channel-count\":2}}");
        assertEquals(List.of(
                "position 1.5",
                "duration 180.25",
                "pause true",
                "decoded s32, 96000 Hz, 2 canales",
                "output float, 48000 Hz, 2 canales"), listener.calls());
    }

    @Test
    public void unavailablePropertiesAreEmpty() {
        // Sin pista, mpv manda el cambio sin «data».
        receive("{\"event\":\"property-change\",\"id\":1,\"name\":\"time-pos\"}");
        receive("{\"event\":\"property-change\",\"id\":2,\"name\":\"duration\"}");
        receive("{\"event\":\"property-change\",\"id\":4,\"name\":\"audio-params\"}");
        receive("{\"event\":\"property-change\",\"id\":5,\"name\":\"audio-out-params\"}");
        assertEquals(List.of("position -", "duration -", "decoded -", "output -"), listener.calls());
    }

    @Test
    public void ignoresOtherEventsAndProperties() {
        receive("{\"event\":\"start-file\",\"playlist_entry_id\":1}");
        receive("{\"event\":\"file-loaded\"}");
        receive("{\"event\":\"idle\"}");
        receive("{\"event\":\"property-change\",\"id\":9,\"name\":\"volume\",\"data\":100}");
        receive("{\"event\":\"property-change\",\"id\":3,\"name\":\"pause\"}");
        assertEquals(List.of(), listener.calls());
    }

    @Test
    public void naturalEndHasNoError() {
        receive("{\"event\":\"end-file\",\"reason\":\"eof\",\"playlist_entry_id\":1}");
        assertEquals(List.of("ended EOF"), listener.calls());
    }

    @Test
    public void errorEndCarriesMpvError() {
        receive("{\"event\":\"end-file\",\"reason\":\"error\",\"playlist_entry_id\":1,"
                + "\"file_error\":\"loading failed\"}");
        assertEquals(List.of("ended ERROR loading failed"), listener.calls());
    }

    @Test
    public void endWithoutReasonIsUnknown() {
        receive("{\"event\":\"end-file\"}");
        assertEquals(List.of("ended UNKNOWN"), listener.calls());
    }

    @Test
    public void endReasons() {
        assertSame(EndReason.EOF, EndReason.from("eof"));
        assertSame(EndReason.STOP, EndReason.from("stop"));
        assertSame(EndReason.QUIT, EndReason.from("quit"));
        assertSame(EndReason.ERROR, EndReason.from("error"));
        assertSame(EndReason.REDIRECT, EndReason.from("redirect"));
        assertSame(EndReason.UNKNOWN, EndReason.from("unknown"));
        assertSame(EndReason.UNKNOWN, EndReason.from("algo-nuevo"));
    }

    @Test
    public void unexpectedCloseIsAFailureWithItsCause() {
        IOException cause = new IOException("se cortó la tubería de mpv");
        events.onClosed(Optional.of(cause));
        assertEquals(List.of("failure"), listener.calls());
        IOException failure = listener.failure().orElseThrow();
        assertTrue(failure.getMessage(), failure.getMessage().contains("mpv"));
        assertSame(cause, failure.getCause());
    }

    @Test
    public void closeWithoutErrorIsStillAFailure() {
        events.onClosed(Optional.empty());
        assertTrue(listener.failure().isPresent());
        assertFalse(listener.failure().orElseThrow().getMessage().isEmpty());
    }
}
