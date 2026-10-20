package io.github.mex040708afk.bitabit.desktop;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotEquals;

import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.security.MessageDigest;
import java.util.HexFormat;
import java.util.Random;
import org.junit.Test;

public class PcmDigestTest {

    /** MD5 de las muestras empaquetadas como FLAC: little-endian, ceil(bits/8) bytes. */
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

    /** Muestras al azar de todo el rango, incluidos los dos extremos. */
    private static long[] randomSamples(int bits, int count, long seed) {
        long max = (1L << (bits - 1)) - 1;
        long min = -(1L << (bits - 1));
        Random random = new Random(seed);
        long[] s = new long[count];
        for (int i = 0; i < count; i++) {
            s[i] = min + (long) (random.nextDouble() * (max - min + 1));
        }
        s[0] = min;
        s[1] = max;
        s[2] = 0;
        return s;
    }

    /** Lo que entrega un decodificador con salida en coma flotante: muestra / 2^(bits−1). */
    private static ByteBuffer asFloats(long[] samples, int bits) {
        ByteBuffer b = ByteBuffer.allocate(samples.length * Float.BYTES).order(ByteOrder.nativeOrder());
        for (long s : samples) {
            b.putFloat((float) (s / (double) (1L << (bits - 1))));
        }
        b.flip(); // en android.jar, flip() devuelve Buffer
        return b;
    }

    private static ByteBuffer asInts(long[] samples, int bits) {
        int width = bits / 8;
        ByteBuffer b = ByteBuffer.allocate(samples.length * width).order(ByteOrder.LITTLE_ENDIAN);
        for (long s : samples) {
            for (int i = 0; i < width; i++) {
                b.put((byte) (s >> (8 * i)));
            }
        }
        b.flip(); // en android.jar, flip() devuelve Buffer
        return b;
    }

    @Test
    public void floatOf24BitIsExact() throws Exception {
        long[] samples = randomSamples(24, 100_000, 1);
        PcmDigest d = new PcmDigest(24);
        d.addFloat(asFloats(samples, 24));
        assertEquals(flacMD5(samples, 24), d.hex());
        assertEquals(samples.length, d.samples());
    }

    @Test
    public void floatOf16BitIsExact() throws Exception {
        long[] samples = randomSamples(16, 100_000, 2);
        PcmDigest d = new PcmDigest(16);
        d.addFloat(asFloats(samples, 16));
        assertEquals(flacMD5(samples, 16), d.hex());
    }

    @Test
    public void floatOutOfRangeIsClipped() throws Exception {
        ByteBuffer b = ByteBuffer.allocate(2 * Float.BYTES).order(ByteOrder.nativeOrder());
        b.putFloat(1.5f).putFloat(-1.5f).flip();
        PcmDigest d = new PcmDigest(24);
        d.addFloat(b);
        assertEquals(flacMD5(new long[] {(1 << 23) - 1, -(1 << 23)}, 24), d.hex());
    }

    @Test
    public void intSamplesAreHashedAsTheyAre() throws Exception {
        for (int bits : new int[] {16, 24, 32}) {
            long[] samples = randomSamples(bits, 10_000, bits);
            PcmDigest d = new PcmDigest(bits);
            d.addInt(asInts(samples, bits), bits);
            assertEquals("bits=" + bits, flacMD5(samples, bits), d.hex());
        }
    }

    // Un FLAC de 24 bits que llegara al sistema de sonido en 16 bits habría
    // perdido información: el MD5 no debe coincidir.
    @Test
    public void truncatedTo16BitDoesNotMatch() throws Exception {
        long[] samples = randomSamples(24, 10_000, 3);
        long[] truncated = new long[samples.length];
        for (int i = 0; i < samples.length; i++) {
            truncated[i] = samples[i] >> 8;
        }
        PcmDigest d = new PcmDigest(24);
        d.addInt(asInts(truncated, 16), 16);
        assertNotEquals(flacMD5(samples, 24), d.hex());
    }

    @Test
    public void chunksAddUpToTheWhole() throws Exception {
        long[] samples = randomSamples(24, 9_999, 4);
        ByteBuffer all = asFloats(samples, 24);
        PcmDigest d = new PcmDigest(24);
        int chunk = 1_000 * Float.BYTES;
        for (int start = 0; start < all.limit(); start += chunk) {
            ByteBuffer part = all.duplicate();
            part.position(start).limit(Math.min(start + chunk, all.limit()));
            d.addFloat(part.slice().order(ByteOrder.nativeOrder()));
        }
        assertEquals(flacMD5(samples, 24), d.hex());
    }

    @Test
    public void doesNotMoveTheBufferAndHexDoesNotReset() throws Exception {
        long[] samples = randomSamples(16, 100, 5);
        ByteBuffer b = asFloats(samples, 16);
        PcmDigest d = new PcmDigest(16);
        d.addFloat(b);
        assertEquals(0, b.position());
        assertEquals(d.hex(), d.hex());

        d.reset();
        assertEquals(0, d.samples());
        assertEquals(flacMD5(new long[0], 16), d.hex());
    }
}
