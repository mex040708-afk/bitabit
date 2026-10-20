package io.github.mex040708afk.bitabit;

import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;

/**
 * Calcula el MD5 del audio en el mismo formato con el que FLAC lo guarda en
 * STREAMINFO: muestras enteras con signo, entrelazadas, en little-endian y con
 * ceil(bits/8) bytes cada una. Así se puede comparar lo que suena con el
 * original guardado en el servidor.
 */
final class PcmDigest {

    private final int bits;
    private final int bytesPerSample;
    private final long minSample;
    private final long maxSample;
    private final double floatScale;
    private final MessageDigest md5;
    private byte[] scratch = new byte[0];
    private long samples;

    PcmDigest(int bits) {
        if (bits < 8 || bits > 32) {
            throw new IllegalArgumentException("bits por muestra no admitidos: " + bits);
        }
        this.bits = bits;
        bytesPerSample = (bits + 7) / 8;
        maxSample = (1L << (bits - 1)) - 1;
        minSample = -(1L << (bits - 1));
        floatScale = 1L << (bits - 1);
        try {
            md5 = MessageDigest.getInstance("MD5");
        } catch (NoSuchAlgorithmException e) {
            // Toda implementación de Java está obligada a traer MD5.
            throw new IllegalStateException("el sistema no tiene MD5", e);
        }
    }

    int bits() {
        return bits;
    }

    long samples() {
        return samples;
    }

    /**
     * Añade muestras en coma flotante de 32 bits (de −1 a 1) en el orden de
     * bytes de la máquina, como las entrega el decodificador con la salida en
     * coma flotante. No mueve la posición de {@code buffer}.
     */
    void addFloat(ByteBuffer buffer) {
        ByteBuffer in = buffer.duplicate().order(ByteOrder.nativeOrder());
        int count = in.remaining() / Float.BYTES;
        byte[] out = scratch(count);
        int o = 0;
        for (int i = 0; i < count; i++) {
            // Si el decodificador dividió entre 2^(bits−1), multiplicar lo
            // devuelve exacto: la mantisa de un float tiene 24 bits.
            long sample = Math.round(in.getFloat() * floatScale);
            o = write(out, o, Math.max(minSample, Math.min(maxSample, sample)));
        }
        md5.update(out, 0, o);
        samples += count;
    }

    /**
     * Añade muestras enteras con signo de {@code sourceBits} bits (16, 24 o 32)
     * en little-endian. Si la fuente tiene menos bits que el digest se rellenan
     * con ceros por la derecha, y el MD5 dejará de coincidir: es justo lo que se
     * quiere detectar. No mueve la posición de {@code buffer}.
     */
    void addInt(ByteBuffer buffer, int sourceBits) {
        if (sourceBits != 16 && sourceBits != 24 && sourceBits != 32) {
            throw new IllegalArgumentException("bits de la fuente no admitidos: " + sourceBits);
        }
        ByteBuffer in = buffer.duplicate().order(ByteOrder.LITTLE_ENDIAN);
        int sourceBytes = sourceBits / 8;
        int count = in.remaining() / sourceBytes;
        byte[] out = scratch(count);
        int o = 0;
        for (int i = 0; i < count; i++) {
            long sample;
            if (sourceBits == 16) {
                sample = in.getShort();
            } else if (sourceBits == 24) {
                int b0 = in.get() & 0xff;
                int b1 = in.get() & 0xff;
                int b2 = in.get(); // con signo: extiende el signo al resto
                sample = (b2 << 16) | (b1 << 8) | b0;
            } else {
                sample = in.getInt();
            }
            int shift = bits - sourceBits;
            sample = shift >= 0 ? sample << shift : sample >> -shift;
            o = write(out, o, sample);
        }
        md5.update(out, 0, o);
        samples += count;
    }

    /** Vuelve a empezar, como si no se hubiera añadido nada. */
    void reset() {
        md5.reset();
        samples = 0;
    }

    /** El MD5 de lo añadido hasta ahora, en hexadecimal. No reinicia la cuenta. */
    String hex() {
        byte[] sum;
        try {
            sum = ((MessageDigest) md5.clone()).digest();
        } catch (CloneNotSupportedException e) {
            // El MD5 de la plataforma se puede clonar en Android y en la JVM.
            throw new IllegalStateException("no se puede copiar el MD5", e);
        }
        StringBuilder s = new StringBuilder(sum.length * 2);
        for (byte b : sum) {
            s.append(Character.forDigit((b >> 4) & 0xf, 16)).append(Character.forDigit(b & 0xf, 16));
        }
        return s.toString();
    }

    private byte[] scratch(int count) {
        int needed = count * bytesPerSample;
        if (scratch.length < needed) {
            scratch = new byte[needed];
        }
        return scratch;
    }

    private int write(byte[] out, int o, long sample) {
        for (int i = 0; i < bytesPerSample; i++) {
            out[o++] = (byte) (sample >> (8 * i));
        }
        return o;
    }
}
