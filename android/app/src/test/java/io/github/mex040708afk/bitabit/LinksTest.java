package io.github.mex040708afk.bitabit;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;

import org.junit.Test;

public class LinksTest {

    private static final String QUERY = "?q=orig&exp=1767225600&kid=k1&sig=abc";

    @Test
    public void usesTheChosenServerWhateverTheLinkSays() {
        String want = "http://10.0.2.2:8080/media/1.flac" + QUERY;
        assertEquals(want, Links.mediaUrl("http://10.0.2.2:8080", "http://localhost:8080/media/1.flac" + QUERY));
        assertEquals(want, Links.mediaUrl("http://10.0.2.2:8080/", "/media/1.flac" + QUERY));
        assertEquals(want, Links.mediaUrl("  http://10.0.2.2:8080//  ", "  /media/1.flac" + QUERY + "\n"));
    }

    @Test
    public void rejectsWhatIsNotAMediaLinkOrAnHttpServer() {
        assertNull(Links.mediaUrl("http://10.0.2.2:8080", "http://localhost:8080/otra/1.flac" + QUERY));
        assertNull(Links.mediaUrl("http://10.0.2.2:8080", ""));
        assertNull(Links.mediaUrl("10.0.2.2:8080", "/media/1.flac" + QUERY));
        assertNull(Links.mediaUrl("file:///sdcard", "/media/1.flac" + QUERY));
    }
}
