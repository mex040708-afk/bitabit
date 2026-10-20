package io.github.mex040708afk.bitabit.desktop;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertThrows;
import static org.junit.Assert.assertTrue;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.HexFormat;
import org.junit.Test;

public class StreamInfoTest {

    private static final String MD5 = "144d6134700b581a11a96f6dfd83ada7";

    /** El principio de un FLAC, escrito a mano campo a campo. */
    private static byte[] flacHead(int sampleRate, int channels, int bits, long totalSamples, String md5) {
        byte[] head = new byte[StreamInfo.SIZE];
        System.arraycopy("fLaC".getBytes(StandardCharsets.US_ASCII), 0, head, 0, 4);
        head[4] = (byte) 0x80; // último bloque de metadatos, tipo 0
        head[7] = 34;
        long packed = ((long) sampleRate << 44) | ((long) (channels - 1) << 41) | ((long) (bits - 1) << 36)
                | totalSamples;
        for (int i = 0; i < 8; i++) {
            head[18 + i] = (byte) (packed >>> (56 - 8 * i));
        }
        System.arraycopy(HexFormat.of().parseHex(md5), 0, head, 26, 16);
        return head;
    }

    @Test
    public void readsTheFields() throws IOException {
        StreamInfo info = StreamInfo.parse(flacHead(96_000, 2, 24, 768_000, MD5));
        assertEquals(new StreamInfo(96_000, 2, 24, 768_000, MD5), info);
        assertTrue(info.hasMd5());
        assertEquals(8.0, info.seconds(), 0);
    }

    @Test
    public void readsTheLimits() throws IOException {
        // 20 bits de frecuencia, 8 canales, 32 bits y 36 bits de muestras.
        StreamInfo info = StreamInfo.parse(flacHead(655_350, 8, 32, 0xf_ffff_ffffL, MD5));
        assertEquals(new StreamInfo(655_350, 8, 32, 0xf_ffff_ffffL, MD5), info);
    }

    @Test
    public void zeroMd5MeansThereIsNone() throws IOException {
        assertFalse(StreamInfo.parse(flacHead(44_100, 1, 16, 1, "0".repeat(32))).hasMd5());
    }

    @Test
    public void rejectsWhatIsNotFlac() {
        byte[] head = flacHead(44_100, 2, 16, 1, MD5);
        head[0] = 'I';
        assertThrows(IOException.class, () -> StreamInfo.parse(head));
    }

    @Test
    public void rejectsAnotherFirstBlock() {
        byte[] head = flacHead(44_100, 2, 16, 1, MD5);
        head[4] = 0x04; // VORBIS_COMMENT
        assertThrows(IOException.class, () -> StreamInfo.parse(head));
    }

    @Test
    public void rejectsAShortFile() {
        byte[] head = new byte[StreamInfo.SIZE - 1];
        System.arraycopy(flacHead(44_100, 2, 16, 1, MD5), 0, head, 0, head.length);
        assertThrows(IOException.class, () -> StreamInfo.parse(head));
    }
}
