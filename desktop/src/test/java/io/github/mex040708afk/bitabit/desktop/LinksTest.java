package io.github.mex040708afk.bitabit.desktop;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;

import java.net.URI;
import org.junit.Test;

public class LinksTest {

    private static final String LINK = "http://localhost:8080/media/1.flac?q=orig&exp=1760968800&kid=k1&sig=abc_-";

    @Test
    public void acceptsAPastedLink() {
        assertEquals(URI.create(LINK), Links.parse("  " + LINK + "\n").orElseThrow());
    }

    @Test
    public void rejectsWhatIsNotASignedMediaLink() {
        for (String bad : new String[] {
                "",
                "localhost:8080/media/1.flac?sig=x",
                "file:///C:/media/1.flac?sig=x",
                "ftp://localhost/media/1.flac?sig=x",
                "http://localhost:8080/otra/1.flac?sig=x",
                "http://localhost:8080/media/1.flac",
                "http://local host/media/1.flac?sig=x",
        }) {
            assertTrue(bad, Links.parse(bad).isEmpty());
        }
    }

    @Test
    public void safeLeavesOutTheSignature() {
        assertEquals("/media/1.flac", Links.safe(URI.create(LINK)));
    }

    @Test
    public void extensionIsLowerCase() {
        assertEquals("flac", Links.extension(URI.create("http://h/media/1.FLAC?sig=x")));
        assertEquals("m4a", Links.extension(URI.create("http://h/media/1.m4a?sig=x")));
    }
}
