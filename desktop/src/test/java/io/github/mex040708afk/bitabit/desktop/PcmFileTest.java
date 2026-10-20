package io.github.mex040708afk.bitabit.desktop;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertThrows;

import java.io.IOException;
import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.MessageDigest;
import java.util.HexFormat;
import java.util.Random;
import org.junit.After;
import org.junit.Test;

public class PcmFileTest {

    private Path file;

    @After
    public void deleteFile() throws IOException {
        if (file != null) {
            Files.deleteIfExists(file);
        }
    }

    private static String flacMD5(long[] samples, int bits) throws Exception {
        int width = (bits + 7) / 8;
        byte[] packed = new byte[samples.length * width];
        for (int i = 0; i < samples.length; i++) {
            for (int b = 0; b < width; b++) {
                packed[i * width + b] = (byte) (samples[i] >> (8 * b));
            }
        }
        return HexFormat.of().formatHex(MessageDigest.getInstance("MD5").digest(packed));
    }

    private static long[] randomSamples(int bits, int count) {
        long max = (1L << (bits - 1)) - 1;
        long min = -(1L << (bits - 1));
        Random random = new Random(bits);
        long[] s = new long[count];
        for (int i = 0; i < count; i++) {
            s[i] = min + (long) (random.nextDouble() * (max - min + 1));
        }
        s[0] = min;
        s[1] = max;
        return s;
    }

    private Path write(ByteBuffer pcm) throws IOException {
        file = Files.createTempFile("bitabit-test-", ".pcm");
        Files.write(file, pcm.array());
        return file;
    }

    // Así entrega mpv un FLAC de 24 bits: en s32, con la muestra en los 24
    // bits altos. Más de 1 MiB, para que haga falta más de una lectura.
    @Test
    public void s32From24BitMatchesFlac() throws Exception {
        long[] samples = randomSamples(24, 300_000);
        ByteBuffer pcm = ByteBuffer.allocate(samples.length * 4).order(ByteOrder.LITTLE_ENDIAN);
        for (long s : samples) {
            pcm.putInt((int) (s << 8));
        }
        PcmDigest d = PcmFile.digest(write(pcm), "s32", 24);
        assertEquals(flacMD5(samples, 24), d.hex());
        assertEquals(samples.length, d.samples());
    }

    @Test
    public void s16MatchesFlac() throws Exception {
        long[] samples = randomSamples(16, 10_000);
        ByteBuffer pcm = ByteBuffer.allocate(samples.length * 2).order(ByteOrder.LITTLE_ENDIAN);
        for (long s : samples) {
            pcm.putShort((short) s);
        }
        assertEquals(flacMD5(samples, 16), PcmFile.digest(write(pcm), "s16", 16).hex());
    }

    @Test
    public void floatMatchesFlac() throws Exception {
        long[] samples = randomSamples(24, 10_000);
        ByteBuffer pcm = ByteBuffer.allocate(samples.length * 4).order(ByteOrder.nativeOrder());
        for (long s : samples) {
            pcm.putFloat((float) (s / (double) (1 << 23)));
        }
        assertEquals(flacMD5(samples, 24), PcmFile.digest(write(pcm), "float", 24).hex());
    }

    @Test
    public void rejectsHalfASample() throws Exception {
        Path pcm = write(ByteBuffer.allocate(7));
        assertThrows(IOException.class, () -> PcmFile.digest(pcm, "s32", 24));
    }

    @Test
    public void rejectsUnknownFormats() throws Exception {
        Path pcm = write(ByteBuffer.allocate(8));
        assertThrows(IOException.class, () -> PcmFile.digest(pcm, "s32p", 24));
    }
}
