package io.github.mex040708afk.bitabit.desktop;

import java.io.IOException;
import java.io.InputStream;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.Arrays;
import java.util.HexFormat;

/**
 * Los datos del bloque STREAMINFO de un FLAC: formato, número de muestras y
 * el MD5 del audio que guardó el codificador.
 *
 * @param md5 el MD5 en hexadecimal; todo ceros si el codificador no lo calculó
 */
record StreamInfo(int sampleRate, int channels, int bits, long totalSamples, String md5) {

    /** «fLaC», la cabecera del bloque y los 34 bytes de STREAMINFO. */
    static final int SIZE = 4 + 4 + 34;

    private static final String NO_MD5 = "0".repeat(32);

    /** Lee STREAMINFO del principio de un archivo FLAC. */
    static StreamInfo parse(byte[] head) throws IOException {
        if (head.length < SIZE) {
            throw new IOException("el FLAC es demasiado corto: " + head.length + " bytes");
        }
        if (!"fLaC".equals(new String(head, 0, 4, StandardCharsets.US_ASCII))) {
            throw new IOException("no es un FLAC: no empieza por «fLaC»");
        }
        int type = head[4] & 0x7f;
        int length = ((head[5] & 0xff) << 16) | ((head[6] & 0xff) << 8) | (head[7] & 0xff);
        if (type != 0 || length != 34) {
            throw new IOException("el primer bloque no es STREAMINFO (tipo " + type + ", " + length + " bytes)");
        }
        // Después de los tamaños de bloque y de trama: 20 bits de frecuencia,
        // 3 de canales − 1, 5 de bits − 1 y 36 de número de muestras.
        long packed = 0;
        for (int i = 18; i < 26; i++) {
            packed = (packed << 8) | (head[i] & 0xff);
        }
        int sampleRate = (int) (packed >>> 44);
        int channels = (int) ((packed >>> 41) & 0x7) + 1;
        int bits = (int) ((packed >>> 36) & 0x1f) + 1;
        long totalSamples = packed & 0xf_ffff_ffffL;
        if (sampleRate == 0) {
            throw new IOException("STREAMINFO con frecuencia 0");
        }
        String md5 = HexFormat.of().formatHex(Arrays.copyOfRange(head, 26, 42));
        return new StreamInfo(sampleRate, channels, bits, totalSamples, md5);
    }

    /**
     * Pide los primeros bytes del FLAC con un Range y lee STREAMINFO. Los
     * errores no llevan la URL, que tiene la firma.
     */
    static StreamInfo fetch(HttpClient http, URI link) throws IOException, InterruptedException {
        HttpRequest request = HttpRequest.newBuilder(link)
                .timeout(Duration.ofSeconds(10))
                .header("Range", "bytes=0-" + (SIZE - 1))
                .build();
        HttpResponse<InputStream> response = http.send(request, HttpResponse.BodyHandlers.ofInputStream());
        try (InputStream body = response.body()) {
            int status = response.statusCode();
            if (status != 200 && status != 206) {
                throw new IOException("el servidor respondió HTTP " + status);
            }
            // Con 200 llega el archivo entero: basta con leer el principio.
            return parse(body.readNBytes(SIZE));
        }
    }

    boolean hasMd5() {
        return !NO_MD5.equals(md5);
    }

    /** Duración en segundos, o 0 si STREAMINFO no dice el número de muestras. */
    double seconds() {
        return totalSamples / (double) sampleRate;
    }

    String describe() {
        return "FLAC " + bits + " bits, " + sampleRate + " Hz, " + channels + (channels == 1 ? " canal" : " canales");
    }
}
