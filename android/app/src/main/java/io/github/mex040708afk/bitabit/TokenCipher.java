package io.github.mex040708afk.bitabit;

import java.security.GeneralSecurityException;

/**
 * Cifra el token de la sesión. Está separado de {@link TokenStore} para probar su lógica
 * en la JVM con un cifrado falso; en el dispositivo es {@link KeystoreTokenCipher}.
 */
interface TokenCipher {

    /** Cifra {@code plain}. El resultado lleva delante todo lo que haga falta para descifrarlo (el IV). */
    byte[] encrypt(byte[] plain) throws GeneralSecurityException;

    /** Descifra lo que devolvió {@link #encrypt}. Falla si la clave ya no es la misma o los datos están dañados. */
    byte[] decrypt(byte[] sealed) throws GeneralSecurityException;

    /** Borra la clave, para que el siguiente {@link #encrypt} empiece con una nueva. */
    void reset() throws GeneralSecurityException;
}
