package io.github.mex040708afk.bitabit.desktop;

import com.google.gson.JsonObject;
import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.Consumer;

/**
 * «Medir»: el MD5 de lo que mpv entrega a la salida de audio, comparado con el
 * de STREAMINFO (ADR-013). mpv escribe ese audio en un archivo temporal con
 * {@code --ao=pcm}, en vez de mandarlo a la tarjeta.
 */
final class Measure {

    private static final int AUDIO_OUT_PARAMS = 1;

    /**
     * @param samples las muestras contadas, sumando todos los canales
     */
    record Result(StreamInfo original, AudioParams output, String md5, long samples) {

        boolean sameFormat() {
            return output.sampleRate() == original.sampleRate() && output.channels() == original.channels();
        }

        /** STREAMINFO puede no decir el número de muestras: entonces no se comprueba. */
        boolean allSamples() {
            return original.totalSamples() == 0 || samples == original.totalSamples() * original.channels();
        }

        boolean matches() {
            return original.hasMd5() && md5.equals(original.md5()) && allSamples() && sameFormat();
        }
    }

    private Measure() {
    }

    static Result run(HttpClient http, String mpv, URI link, Consumer<String> progress)
            throws IOException, InterruptedException {
        progress.accept("Leyendo STREAMINFO de " + Links.safe(link) + "…");
        StreamInfo original = StreamInfo.fetch(http, link);
        Path pcm = Files.createTempFile("bitabit-", ".pcm");
        Result result;
        try {
            progress.accept("Decodificando " + Links.safe(link) + " con mpv…");
            AudioParams output = decode(mpv, link, pcm, original);
            progress.accept("Calculando el MD5…");
            PcmDigest digest = PcmFile.digest(pcm, output.format(), original.bits());
            result = new Result(original, output, digest.hex(), digest.samples());
        } catch (IOException | InterruptedException | RuntimeException e) {
            try {
                Files.deleteIfExists(pcm);
            } catch (IOException deleting) {
                e.addSuppressed(deleting);
            }
            throw e;
        }
        Files.delete(pcm);
        return result;
    }

    private static AudioParams decode(String exe, URI link, Path pcm, StreamInfo original)
            throws IOException, InterruptedException {
        CompletableFuture<JsonObject> end = new CompletableFuture<>();
        AtomicReference<AudioParams> output = new AtomicReference<>();
        Mpv.Listener listener = new Mpv.Listener() {
            @Override
            public void onEvent(JsonObject event) {
                switch (event.get("event").getAsString()) {
                    case "property-change" -> {
                        if (event.get("id").getAsInt() == AUDIO_OUT_PARAMS) {
                            AudioParams.from(event.get("data")).ifPresent(output::set);
                        }
                    }
                    case "end-file" -> end.complete(event);
                    default -> {
                        // El resto de eventos no hacen falta para medir.
                    }
                }
            }

            @Override
            public void onClosed(Optional<IOException> error) {
                end.completeExceptionally(error.orElseGet(() -> new IOException("mpv se cerró antes de terminar")));
            }
        };
        List<String> options = List.of("--ao=pcm", "--ao-pcm-waveheader=no", "--ao-pcm-file=" + pcm);
        // Al cerrar, mpv sale y cierra el archivo: hasta entonces puede no estar entero.
        try (Mpv mpv = Mpv.start(exe, options, listener)) {
            mpv.observe(AUDIO_OUT_PARAMS, "audio-out-params");
            mpv.load(link);
            // Con --ao=pcm, mpv va todo lo rápido que puede: la duración de la
            // pista sobra como margen.
            long timeout = 60 + (long) original.seconds();
            JsonObject event = end.get(timeout, TimeUnit.SECONDS);
            String reason = event.has("reason") ? event.get("reason").getAsString() : "?";
            if (!reason.equals("eof")) {
                String error = event.has("file_error") ? " (" + event.get("file_error").getAsString() + ")" : "";
                throw new IOException("mpv no terminó la pista: " + reason + error);
            }
        } catch (ExecutionException e) {
            throw new IOException(e.getCause().getMessage(), e.getCause());
        } catch (TimeoutException e) {
            throw new IOException("mpv no terminó la pista a tiempo", e);
        }
        AudioParams params = output.get();
        if (params == null) {
            throw new IOException("mpv no dijo en qué formato entregó el audio");
        }
        return params;
    }
}
