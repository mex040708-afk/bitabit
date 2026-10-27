package io.github.mex040708afk.bitabit.desktop;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;

import java.io.IOException;
import java.net.ConnectException;
import java.net.SocketTimeoutException;
import java.net.UnknownHostException;
import org.junit.Test;

public class MessagesTest {

    private static final String SERVER = "http://servidor:8080";

    @Test
    public void durationRoundsUpWhenThereAreHours() {
        assertEquals("1 s", Messages.duration(0));
        assertEquals("40 s", Messages.duration(40));
        assertEquals("1 min", Messages.duration(60));
        assertEquals("5 min 3 s", Messages.duration(303));
        assertEquals("1 h", Messages.duration(3600));
        assertEquals("1 h 1 min", Messages.duration(3601));
        assertEquals("2 h", Messages.duration(2 * 3600 - 1));
        assertEquals("24 h", Messages.duration(86400));
        assertEquals("23 h 59 min", Messages.duration(86340));
    }

    @Test
    public void networkErrorsSayWhatHappened() {
        assertTrue(Messages.request(SERVER, new UnknownHostException("servidor")).startsWith("No se encuentra"));
        assertTrue(Messages.request(SERVER, new ConnectException("Connection refused")).startsWith("No se pudo conectar"));
        assertTrue(Messages.request(SERVER, new SocketTimeoutException("timeout")).contains("a tiempo"));
        assertEquals("Error de red con " + SERVER + ": IOException (algo).", Messages.request(SERVER, new IOException("algo")));
    }
}
