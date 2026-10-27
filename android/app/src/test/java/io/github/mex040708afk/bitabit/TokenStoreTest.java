package io.github.mex040708afk.bitabit;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import java.security.GeneralSecurityException;
import java.util.ArrayList;
import java.util.List;
import java.util.Optional;
import javax.crypto.AEADBadTagException;
import org.junit.Before;
import org.junit.Test;

public class TokenStoreTest {

    private static final String TOKEN = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ";
    private static final String SERVER = "http://10.0.2.2:8080";

    private FakePreferences prefs;
    private FakeCipher cipher;
    private List<String> warnings;
    private TokenStore store;

    @Before
    public void setUp() {
        prefs = new FakePreferences();
        cipher = new FakeCipher();
        warnings = new ArrayList<>();
        store = new TokenStore(prefs, cipher, warnings::add);
    }

    @Test
    public void savesAndLoadsTheSession() throws GeneralSecurityException {
        store.save(SERVER, 7, TOKEN);

        TokenStore.SavedSession saved = store.load().orElseThrow();
        assertEquals(SERVER, saved.server());
        assertEquals(7, saved.sessionId());
        assertEquals(TOKEN, saved.token());
        assertTrue(warnings.isEmpty());
    }

    @Test
    public void neverStoresTheTokenInClear() throws GeneralSecurityException {
        store.save(SERVER, 7, TOKEN);

        for (Object value : prefs.values.values()) {
            assertFalse(String.valueOf(value).contains(TOKEN));
        }
    }

    @Test
    public void emptyWhenNothingSaved() {
        assertEquals(Optional.empty(), store.load());
        assertEquals(Optional.empty(), store.lastServer());
        assertTrue(warnings.isEmpty());
    }

    @Test
    public void failedDecryptionDiscardsTheSessionAndTheKey() throws GeneralSecurityException {
        store.save(SERVER, 7, TOKEN);
        cipher.failDecrypt = true;

        assertEquals(Optional.empty(), store.load());

        assertEquals(1, cipher.resets);
        assertEquals(1, warnings.size());
        assertTrue(warnings.get(0).contains("AEADBadTagException"));
        cipher.failDecrypt = false;
        assertEquals(Optional.empty(), store.load());
        // El servidor no es secreto: se queda para no tener que escribirlo otra vez.
        assertEquals(Optional.of(SERVER), store.lastServer());
    }

    @Test
    public void corruptBase64DiscardsTheSession() throws GeneralSecurityException {
        store.save(SERVER, 7, TOKEN);
        prefs.values.put("token", "esto no es base64 ***");

        assertEquals(Optional.empty(), store.load());
        assertEquals(1, cipher.resets);
        assertFalse(prefs.values.containsKey("token"));
    }

    @Test
    public void incompleteSessionIsDiscarded() throws GeneralSecurityException {
        store.save(SERVER, 7, TOKEN);
        prefs.values.remove("session_id");

        assertEquals(Optional.empty(), store.load());
        assertFalse(prefs.values.containsKey("token"));
        assertEquals(1, warnings.size());
    }

    @Test
    public void failedResetIsReportedNotHidden() throws GeneralSecurityException {
        store.save(SERVER, 7, TOKEN);
        cipher.failDecrypt = true;
        cipher.failReset = true;

        assertEquals(Optional.empty(), store.load());
        assertEquals(2, warnings.size());
        assertFalse(prefs.values.containsKey("token"));
    }

    @Test
    public void failedEncryptionSavesNothing() {
        cipher.failEncrypt = true;
        try {
            store.save(SERVER, 7, TOKEN);
            fail("tenía que fallar");
        } catch (GeneralSecurityException expected) {
            assertTrue(prefs.values.isEmpty());
        }
    }

    @Test
    public void clearForgetsTokenButKeepsServer() throws GeneralSecurityException {
        store.save(SERVER, 7, TOKEN);

        store.clear();

        assertEquals(Optional.empty(), store.load());
        assertEquals(Optional.of(SERVER), store.lastServer());
        assertEquals(0, cipher.resets);
    }

    /** Un «cifrado» reversible con un IV delante, como el real. No protege nada: solo sirve para probar. */
    private static final class FakeCipher implements TokenCipher {

        boolean failEncrypt;
        boolean failDecrypt;
        boolean failReset;
        int resets;

        @Override
        public byte[] encrypt(byte[] plain) throws GeneralSecurityException {
            if (failEncrypt) {
                throw new GeneralSecurityException("falso");
            }
            byte[] sealed = new byte[plain.length + 1];
            sealed[0] = 0x5a;
            for (int i = 0; i < plain.length; i++) {
                sealed[i + 1] = (byte) (plain[i] ^ sealed[0]);
            }
            return sealed;
        }

        @Override
        public byte[] decrypt(byte[] sealed) throws GeneralSecurityException {
            if (failDecrypt) {
                throw new AEADBadTagException("falso");
            }
            byte[] plain = new byte[sealed.length - 1];
            for (int i = 0; i < plain.length; i++) {
                plain[i] = (byte) (sealed[i + 1] ^ sealed[0]);
            }
            return plain;
        }

        @Override
        public void reset() throws GeneralSecurityException {
            resets++;
            if (failReset) {
                throw new GeneralSecurityException("falso");
            }
        }
    }
}
