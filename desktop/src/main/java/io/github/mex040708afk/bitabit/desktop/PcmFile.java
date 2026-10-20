package io.github.mex040708afk.bitabit.desktop;

import java.io.IOException;
import java.nio.ByteBuffer;
import java.nio.channels.FileChannel;
import java.nio.file.Path;
import java.nio.file.StandardOpenOption;

/** Calcula el MD5 del PCM sin cabecera que escribe mpv con {@code --ao=pcm}. */
final class PcmFile {

    private PcmFile() {
    }

    /**
     * @param format el formato de mpv con el que se escribió ({@code audio-out-params/format})
     * @param bits los bits por muestra del original, que mpv no da
     */
    static PcmDigest digest(Path file, String format, int bits) throws IOException {
        int sampleBytes = switch (format) {
            case "s16" -> 2;
            case "s32", "float" -> 4;
            default -> throw new IOException("formato de mpv no admitido: " + format);
        };
        PcmDigest digest = new PcmDigest(bits);
        ByteBuffer buffer = ByteBuffer.allocate(1 << 20);
        try (FileChannel in = FileChannel.open(file, StandardOpenOption.READ)) {
            while (in.read(buffer) >= 0) {
                buffer.flip();
                // Una lectura puede acabar a mitad de una muestra: lo que sobra
                // se guarda para la siguiente vuelta.
                int whole = buffer.remaining() - buffer.remaining() % sampleBytes;
                ByteBuffer samples = buffer.slice(buffer.position(), whole);
                if (format.equals("float")) {
                    digest.addFloat(samples);
                } else {
                    digest.addInt(samples, sampleBytes * 8);
                }
                buffer.position(buffer.position() + whole);
                buffer.compact();
            }
        }
        if (buffer.position() != 0) {
            throw new IOException("el PCM termina a mitad de una muestra");
        }
        return digest;
    }
}
