package io.github.mex040708afk.bitabit;

import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;
import java.io.IOException;
import java.security.GeneralSecurityException;
import java.security.Key;
import java.security.KeyStore;
import java.security.KeyStoreException;
import java.util.Arrays;
import javax.crypto.Cipher;
import javax.crypto.KeyGenerator;
import javax.crypto.spec.GCMParameterSpec;

/**
 * AES-256-GCM con una clave del Android Keystore que nunca sale de él (ADR-021). Lo
 * cifrado es el IV de 12 bytes seguido del texto cifrado con su etiqueta.
 */
final class KeystoreTokenCipher implements TokenCipher {

    private static final String PROVIDER = "AndroidKeyStore";
    private static final String ALIAS = "bitabit-token";
    private static final String TRANSFORMATION = "AES/GCM/NoPadding";
    private static final int IV_BYTES = 12;
    private static final int TAG_BITS = 128;

    @Override
    public byte[] encrypt(byte[] plain) throws GeneralSecurityException {
        Cipher cipher = Cipher.getInstance(TRANSFORMATION);
        // El Keystore elige el IV: no deja usar uno propio con una clave de cifrado aleatorio.
        cipher.init(Cipher.ENCRYPT_MODE, key(true));
        byte[] iv = cipher.getIV();
        if (iv.length != IV_BYTES) {
            throw new GeneralSecurityException("IV inesperado de " + iv.length + " bytes");
        }
        byte[] body = cipher.doFinal(plain);
        byte[] sealed = Arrays.copyOf(iv, IV_BYTES + body.length);
        System.arraycopy(body, 0, sealed, IV_BYTES, body.length);
        return sealed;
    }

    @Override
    public byte[] decrypt(byte[] sealed) throws GeneralSecurityException {
        if (sealed.length <= IV_BYTES) {
            throw new GeneralSecurityException("datos cifrados demasiado cortos");
        }
        Cipher cipher = Cipher.getInstance(TRANSFORMATION);
        cipher.init(Cipher.DECRYPT_MODE, key(false), new GCMParameterSpec(TAG_BITS, sealed, 0, IV_BYTES));
        return cipher.doFinal(sealed, IV_BYTES, sealed.length - IV_BYTES);
    }

    @Override
    public void reset() throws GeneralSecurityException {
        keyStore().deleteEntry(ALIAS);
    }

    private static Key key(boolean create) throws GeneralSecurityException {
        KeyStore store = keyStore();
        Key existing = store.getKey(ALIAS, null);
        if (existing != null) {
            return existing;
        }
        if (!create) {
            throw new KeyStoreException("no hay clave para descifrar el token");
        }
        KeyGenerator generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, PROVIDER);
        generator.init(new KeyGenParameterSpec.Builder(ALIAS,
                KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build());
        return generator.generateKey();
    }

    private static KeyStore keyStore() throws GeneralSecurityException {
        KeyStore store = KeyStore.getInstance(PROVIDER);
        try {
            store.load(null);
        } catch (IOException e) {
            throw new KeyStoreException("no se pudo abrir el Android Keystore", e);
        }
        return store;
    }
}
