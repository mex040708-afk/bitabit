package io.github.mex040708afk.bitabit.desktop;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.fail;

import org.junit.Test;

public class ServerAddressTest {

    @Test
    public void normalizesWhatIsTyped() {
        assertEquals("http://servidor:8080", ServerAddress.parse("  http://servidor:8080\n"));
        assertEquals("http://servidor:8080", ServerAddress.parse("HTTP://Servidor:8080/"));
        assertEquals("https://192.168.1.10", ServerAddress.parse("https://192.168.1.10"));
        assertEquals("http://[::1]:8080", ServerAddress.parse("http://[::1]:8080"));
    }

    @Test
    public void rejectsWhatIsNotJustAServer() {
        for (String bad : new String[] {
                "",
                "   ",
                "servidor:8080",
                "ftp://servidor:8080",
                "file:///C:/bitabit",
                "http://",
                "http://:8080",
                "http://servidor:0",
                "http://servidor:65536",
                "http://servidor:8080/api",
                "http://servidor:8080?x=1",
                "http://servidor:8080#arriba",
                "http://usuario:clave@servidor:8080",
                "http://servi dor:8080",
                "http://servidor:80a",
                "http://" + "a".repeat(3000) + ":8080",
        }) {
            try {
                ServerAddress.parse(bad);
                fail("debería rechazar «" + bad + "»");
            } catch (IllegalArgumentException e) {
                // El texto se enseña tal cual: no puede venir vacío.
                assertEquals(bad, false, e.getMessage().isBlank());
            }
        }
    }
}
