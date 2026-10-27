package io.github.mex040708afk.bitabit.desktop;

import static org.junit.Assert.assertEquals;

import org.junit.Test;

/** Lo que se puede probar del Player sin mpv. */
public class PlayerTest {

    @Test
    public void describesOnlyThePath() {
        assertEquals("/media/42.flac",
                Player.describe("https://musica.example/media/42.flac?exp=1700000000&sig=abcdef0123"));
        assertEquals("/media/7.m4a", Player.describe("http://127.0.0.1:8080/media/7.m4a?exp=1&sig=x"));
    }

    @Test
    public void describesAnythingElseWithoutRepeatingIt() {
        assertEquals("la pista", Player.describe("C:\\música\\tono.flac"));
        assertEquals("la pista", Player.describe("no es un enlace ?sig=secreta"));
        assertEquals("la pista", Player.describe("file:///C:/tono.flac?sig=secreta"));
        assertEquals("la pista", Player.describe("https://host?sig=secreta"));
    }
}
