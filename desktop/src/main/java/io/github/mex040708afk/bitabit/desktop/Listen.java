package io.github.mex040708afk.bitabit.desktop;

import com.google.gson.JsonObject;
import java.io.IOException;
import java.net.URI;
import java.util.List;
import java.util.Optional;

/**
 * «Escuchar»: la pista suena por la tarjeta y se comparan el formato que da el
 * decodificador y el que mpv manda a Windows (ADR-013).
 */
final class Listen implements AutoCloseable {

    /** Lo que se cuenta a la ventana, desde el hilo que lee la tubería de mpv. */
    interface View {

        /** Cambió uno de los dos formatos. Mientras no se conocen, están vacíos. */
        void formats(Optional<AudioParams> decoded, Optional<AudioParams> output);

        /**
         * Terminó la pista o se cerró mpv.
         *
         * @param reason el motivo de mpv: {@code eof} si sonó entera
         */
        void finished(String reason, Optional<IOException> error);
    }

    private static final int AUDIO_PARAMS = 1;
    private static final int AUDIO_OUT_PARAMS = 2;

    private final Mpv mpv;

    private Listen(Mpv mpv) {
        this.mpv = mpv;
    }

    static Listen start(String exe, URI link, boolean exclusive, View view) throws IOException, InterruptedException {
        Mpv mpv = Mpv.start(exe, List.of("--ao=wasapi", "--audio-exclusive=" + (exclusive ? "yes" : "no")),
                new Mpv.Listener() {
                    private Optional<AudioParams> decoded = Optional.empty();
                    private Optional<AudioParams> output = Optional.empty();

                    @Override
                    public void onEvent(JsonObject event) {
                        switch (event.get("event").getAsString()) {
                            case "property-change" -> {
                                Optional<AudioParams> params = AudioParams.from(event.get("data"));
                                if (params.isEmpty()) {
                                    // Al terminar la pista mpv las vacía: se queda el último formato.
                                    return;
                                }
                                int id = event.get("id").getAsInt();
                                if (id == AUDIO_PARAMS) {
                                    decoded = params;
                                } else if (id == AUDIO_OUT_PARAMS) {
                                    output = params;
                                }
                                view.formats(decoded, output);
                            }
                            case "end-file" -> {
                                String reason = event.has("reason") ? event.get("reason").getAsString() : "?";
                                Optional<IOException> error = event.has("file_error")
                                        ? Optional.of(new IOException("mpv: " + event.get("file_error").getAsString()))
                                        : Optional.empty();
                                view.finished(reason, error);
                            }
                            default -> {
                                // El resto de eventos no hacen falta para escuchar.
                            }
                        }
                    }

                    @Override
                    public void onClosed(Optional<IOException> error) {
                        view.finished("cerrado", error);
                    }
                });
        try {
            mpv.observe(AUDIO_PARAMS, "audio-params");
            mpv.observe(AUDIO_OUT_PARAMS, "audio-out-params");
            mpv.load(link);
        } catch (IOException | InterruptedException | RuntimeException e) {
            try {
                mpv.close();
            } catch (IOException closing) {
                e.addSuppressed(closing);
            }
            throw e;
        }
        return new Listen(mpv);
    }

    @Override
    public void close() throws IOException {
        mpv.close();
    }
}
