package io.github.mex040708afk.bitabit;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

public class LoginRulesTest {

    @Test
    public void deviceNameIsTheModelTrimmed() {
        assertEquals("Pixel 7a", LoginRules.deviceName("  Pixel 7a "));
    }

    @Test
    public void emptyModelGetsAName() {
        assertEquals("Android", LoginRules.deviceName(""));
        assertEquals("Android", LoginRules.deviceName("   "));
        assertEquals("Android", LoginRules.deviceName(null));
    }

    @Test
    public void longModelIsCutTo64Characters() {
        String name = LoginRules.deviceName("ñ".repeat(70));
        assertEquals(64, name.codePointCount(0, name.length()));
        // Los caracteres fuera del plano básico cuentan como uno, como en el contrato.
        String emoji = LoginRules.deviceName("🎵".repeat(70));
        assertEquals(64, emoji.codePointCount(0, emoji.length()));
    }

    @Test
    public void supportsOnlyApiOne() {
        assertTrue(LoginRules.supportsApi("1.0.0"));
        assertTrue(LoginRules.supportsApi("1.3"));
        assertTrue(LoginRules.supportsApi("1"));
        assertFalse(LoginRules.supportsApi("2.0.0"));
        assertFalse(LoginRules.supportsApi("10.0.0"));
        assertFalse(LoginRules.supportsApi(""));
        assertFalse(LoginRules.supportsApi(null));
    }
}
