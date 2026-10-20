package io.github.mex040708afk.bitabit.desktop;

import static org.junit.Assert.assertTrue;
import static org.junit.Assume.assumeTrue;

import com.google.gson.JsonObject;
import java.io.IOException;
import java.util.List;
import java.util.Optional;
import org.junit.Test;

/** Habla con un mpv de verdad. Solo corre si BITABIT_MPV dice dónde está. */
public class MpvTest {

    private static final Mpv.Listener IGNORE = new Mpv.Listener() {
        @Override
        public void onEvent(JsonObject event) {
            // Esta prueba solo mira las respuestas.
        }

        @Override
        public void onClosed(Optional<IOException> error) {
            // La cierra la propia prueba.
        }
    };

    @Test
    public void answersCommandsThroughThePipe() throws Exception {
        String exe = System.getenv("BITABIT_MPV");
        assumeTrue("sin BITABIT_MPV", exe != null);
        try (Mpv mpv = Mpv.start(exe, List.of("--ao=null"), IGNORE)) {
            // Varias seguidas: con la lectura siempre pendiente, las escrituras
            // no se pueden quedar bloqueadas.
            for (int i = 0; i < 20; i++) {
                String version = mpv.command("get_property", "mpv-version").getAsString();
                assertTrue(version, version.startsWith("mpv "));
            }
        }
    }

    @Test(expected = IOException.class)
    public void reportsMpvErrors() throws Exception {
        String exe = System.getenv("BITABIT_MPV");
        assumeTrue("sin BITABIT_MPV", exe != null);
        try (Mpv mpv = Mpv.start(exe, List.of("--ao=null"), IGNORE)) {
            mpv.command("get_property", "no-existe");
        }
    }
}
