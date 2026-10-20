package io.github.mex040708afk.bitabit;

import androidx.annotation.OptIn;
import androidx.media3.common.C;
import androidx.media3.common.util.UnstableApi;
import androidx.media3.exoplayer.audio.AudioSink;
import androidx.media3.exoplayer.audio.ForwardingAudioSink;
import java.nio.ByteBuffer;

/**
 * Cuenta en un MD5 todo el audio que el reproductor entrega para sonar, antes
 * de que el sistema de sonido lo procese. Como los bits de la pista no se
 * conocen aquí, lleva a la vez la cuenta para 16 y para 24 bits; quien lo use
 * elige la que corresponde a la pista.
 *
 * <p>Corre en el hilo de reproducción; los métodos públicos están sincronizados
 * para poder leer el resultado desde la interfaz.
 */
@OptIn(markerClass = UnstableApi.class)
final class DigestingAudioSink extends ForwardingAudioSink {

    private final PcmDigest digest16 = new PcmDigest(16);
    private final PcmDigest digest24 = new PcmDigest(24);
    private @C.PcmEncoding int encoding = C.ENCODING_INVALID;
    // handleBuffer recibe el mismo búfer hasta que el sistema de sonido lo
    // acepta entero: solo se cuenta la primera vez.
    private ByteBuffer pending;
    private boolean interrupted;

    DigestingAudioSink(AudioSink sink) {
        super(sink);
    }

    @Override
    public synchronized void configure(AudioSinkConfig config) throws ConfigurationException {
        encoding = config.format.pcmEncoding;
        super.configure(config);
    }

    @Override
    public synchronized boolean handleBuffer(ByteBuffer buffer, long presentationTimeUs, int encodedAccessUnitCount)
            throws InitializationException, WriteException {
        if (buffer != pending) {
            add(buffer);
        }
        boolean handled = super.handleBuffer(buffer, presentationTimeUs, encodedAccessUnitCount);
        pending = handled ? null : buffer;
        return handled;
    }

    @Override
    public synchronized void flush() {
        // La primera vez es el arranque; las demás, un salto dentro de la pista.
        if (digest16.samples() > 0) {
            interrupted = true;
        }
        digest16.reset();
        digest24.reset();
        pending = null;
        super.flush();
    }

    /** El MD5 de lo que ha sonado, para una pista de {@code bits} bits (16 o 24). */
    synchronized String md5(int bits) {
        return (bits == 24 ? digest24 : digest16).hex();
    }

    synchronized long samples() {
        return digest16.samples();
    }

    /** Si hubo un salto: entonces el MD5 solo cuenta desde el último. */
    synchronized boolean interrupted() {
        return interrupted;
    }

    /** El formato en que llegó el audio al sistema de sonido, como texto. */
    synchronized String encodingName() {
        switch (encoding) {
            case C.ENCODING_PCM_FLOAT:
                return "coma flotante de 32 bits";
            case C.ENCODING_PCM_16BIT:
                return "entero de 16 bits";
            case C.ENCODING_PCM_24BIT:
                return "entero de 24 bits";
            case C.ENCODING_PCM_32BIT:
                return "entero de 32 bits";
            default:
                return "desconocido (" + encoding + ")";
        }
    }

    private void add(ByteBuffer buffer) {
        switch (encoding) {
            case C.ENCODING_PCM_FLOAT:
                digest16.addFloat(buffer);
                digest24.addFloat(buffer);
                break;
            case C.ENCODING_PCM_16BIT:
                digest16.addInt(buffer, 16);
                digest24.addInt(buffer, 16);
                break;
            case C.ENCODING_PCM_24BIT:
                digest16.addInt(buffer, 24);
                digest24.addInt(buffer, 24);
                break;
            case C.ENCODING_PCM_32BIT:
                digest16.addInt(buffer, 32);
                digest24.addInt(buffer, 32);
                break;
            default:
                // Formato que no sabemos contar: el MD5 no saldrá y se verá.
                break;
        }
    }
}
