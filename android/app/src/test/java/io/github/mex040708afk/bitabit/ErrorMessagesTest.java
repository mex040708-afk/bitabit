package io.github.mex040708afk.bitabit;

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import java.io.IOException;
import java.net.ConnectException;
import java.util.Optional;
import org.junit.Test;

public class ErrorMessagesTest {

    @Test
    public void wrongPasswordSaysAttemptsLeft() {
        ErrorMessages.Message m = ErrorMessages.describeApi(401, "wrong_password", Optional.of(2), Optional.empty());
        assertEquals(R.plurals.error_wrong_password_attempts, m.resId);
        assertTrue(m.plural);
        assertEquals(2, m.quantity);
        assertArrayEquals(new Object[] {2}, m.args);
    }

    @Test
    public void wrongPasswordWithoutCount() {
        ErrorMessages.Message m = ErrorMessages.describeApi(401, "wrong_password", Optional.empty(), Optional.empty());
        assertEquals(R.string.error_wrong_password, m.resId);
        assertFalse(m.plural);
    }

    @Test
    public void unreadableBodyWithAttemptsIsStillWrongPassword() {
        ErrorMessages.Message m = ErrorMessages.describeApi(401, "", Optional.of(1), Optional.empty());
        assertEquals(R.plurals.error_wrong_password_attempts, m.resId);
    }

    @Test
    public void rejectedTokenAsksToLogInAgain() {
        ErrorMessages.Message m = ErrorMessages.describeApi(401, "unauthorized", Optional.empty(), Optional.empty());
        assertEquals(R.string.error_unauthorized, m.resId);
    }

    @Test
    public void blockedSaysSecondsLeft() {
        ErrorMessages.Message m = ErrorMessages.describeApi(429, "blocked", Optional.empty(), Optional.of(300));
        assertEquals(R.plurals.error_blocked_seconds, m.resId);
        assertEquals(300, m.quantity);
    }

    @Test
    public void blockedWithoutSeconds() {
        ErrorMessages.Message m = ErrorMessages.describeApi(429, "", Optional.empty(), Optional.empty());
        assertEquals(R.string.error_blocked, m.resId);
    }

    @Test
    public void otherErrorsShowStatusAndCode() {
        ErrorMessages.Message m = ErrorMessages.describeApi(500, "internal", Optional.empty(), Optional.empty());
        assertEquals(R.string.error_server, m.resId);
        assertArrayEquals(new Object[] {500, "internal"}, m.args);
        assertArrayEquals(new Object[] {502, "?"},
                ErrorMessages.describeApi(502, "", Optional.empty(), Optional.empty()).args);
    }

    @Test
    public void badServerAddressIsShownAsInvalid() {
        ErrorMessages.Message m = ErrorMessages.describe(
                new IllegalArgumentException("dirección de servidor no válida: ftp://x"));
        assertEquals(R.string.error_invalid, m.resId);
        assertArrayEquals(new Object[] {"dirección de servidor no válida: ftp://x"}, m.args);
    }

    @Test
    public void connectionErrors() {
        assertEquals(R.string.error_network,
                ErrorMessages.describe(new ConnectException("Failed to connect")).resId);
        assertArrayEquals(new Object[] {"IOException"}, ErrorMessages.describe(new IOException()).args);
    }

    @Test
    public void incompatibleServer() {
        ErrorMessages.Message m = ErrorMessages.describe(new IncompatibleServerException("2.0.0"));
        assertEquals(R.string.error_incompatible, m.resId);
        assertArrayEquals(new Object[] {"2.0.0"}, m.args);
    }

    @Test
    public void onlyApiErrorsRejectTheSession() {
        assertFalse(ErrorMessages.isSessionRejected(new IOException("401")));
    }
}
