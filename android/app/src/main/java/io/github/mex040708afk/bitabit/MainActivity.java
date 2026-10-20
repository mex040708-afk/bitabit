package io.github.mex040708afk.bitabit;

import android.app.Activity;
import android.content.Context;
import android.content.Intent;
import android.net.Uri;
import android.os.Bundle;
import android.util.Log;
import android.widget.Button;
import android.widget.EditText;
import android.widget.TextView;
import androidx.annotation.OptIn;
import androidx.media3.common.C;
import androidx.media3.common.Format;
import androidx.media3.common.MediaItem;
import androidx.media3.common.PlaybackException;
import androidx.media3.common.Player;
import androidx.media3.common.Tracks;
import androidx.media3.common.util.UnstableApi;
import androidx.media3.datasource.HttpDataSource;
import androidx.media3.exoplayer.DefaultRenderersFactory;
import androidx.media3.exoplayer.ExoPlayer;
import androidx.media3.exoplayer.audio.AudioSink;
import androidx.media3.exoplayer.audio.DefaultAudioSink;

/**
 * App mínima de la fase 1 (ADR-012): reproduce un enlace de «bitabit link» y,
 * al terminar, enseña el MD5 de lo que ha sonado para compararlo con el del
 * original en el servidor.
 */
@OptIn(markerClass = UnstableApi.class)
public final class MainActivity extends Activity {

    private static final String TAG = "Bitabit";
    /** Extra para pasar el enlace al arrancar, en las pruebas con adb. */
    private static final String EXTRA_LINK = "enlace";
    /** Extra para empezar a reproducir sin pulsar el botón. */
    private static final String EXTRA_PLAY = "reproducir";

    private EditText server;
    private EditText link;
    private TextView status;
    private TextView result;
    private ExoPlayer player;
    private DigestingAudioSink sink;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_main);
        server = findViewById(R.id.server);
        link = findViewById(R.id.link);
        status = findViewById(R.id.status);
        result = findViewById(R.id.result);
        Button play = findViewById(R.id.play);
        Button stop = findViewById(R.id.stop);
        play.setOnClickListener(v -> play());
        stop.setOnClickListener(v -> stop());

        Intent intent = getIntent();
        String extraLink = intent.getStringExtra(EXTRA_LINK);
        if (extraLink != null) {
            link.setText(extraLink);
            if (intent.getBooleanExtra(EXTRA_PLAY, false)) {
                play();
            }
        }
    }

    @Override
    protected void onDestroy() {
        releasePlayer();
        super.onDestroy();
    }

    private void play() {
        String url = Links.mediaUrl(server.getText().toString(), link.getText().toString());
        if (url == null) {
            status.setText(R.string.bad_link);
            return;
        }
        releasePlayer();
        result.setText("");

        sink = new DigestingAudioSink(new DefaultAudioSink.Builder(this)
                // Sin esto, el audio de 24 bits se recortaría a 16.
                .setEnableFloatOutput(true)
                .build());
        DefaultRenderersFactory renderers = new DefaultRenderersFactory(this) {
            @Override
            protected AudioSink buildAudioSink(Context context, boolean enableFloatOutput,
                    boolean enableAudioOutputPlaybackParameters) {
                return sink;
            }
        };
        player = new ExoPlayer.Builder(this, renderers).build();
        player.addListener(new Listener());
        player.setMediaItem(MediaItem.fromUri(url));
        player.prepare();
        player.play();

        // Nunca la consulta: lleva la firma.
        String path = Uri.parse(url).getPath();
        status.setText(getString(R.string.playing, path));
        Log.i(TAG, "reproduciendo " + path);
    }

    private void stop() {
        releasePlayer();
        status.setText(R.string.stopped);
    }

    private void releasePlayer() {
        if (player != null) {
            player.release();
            player = null;
        }
    }

    /** El formato de la pista de audio que se va a reproducir, o null si no hay. */
    static Format selectedAudioFormat(Tracks tracks) {
        for (Tracks.Group group : tracks.getGroups()) {
            if (group.getType() != C.TRACK_TYPE_AUDIO) {
                continue;
            }
            for (int i = 0; i < group.length; i++) {
                if (group.isTrackSelected(i)) {
                    return group.getTrackFormat(i);
                }
            }
        }
        return null;
    }

    /** Bits por muestra de la pista, o 0 si el formato no los dice. */
    static int trackBits(Format format) {
        if (format == null) {
            return 0;
        }
        switch (format.pcmEncoding) {
            case C.ENCODING_PCM_16BIT:
                return 16;
            case C.ENCODING_PCM_24BIT:
                return 24;
            case C.ENCODING_PCM_32BIT:
                return 32;
            default:
                return 0;
        }
    }

    private final class Listener implements Player.Listener {

        @Override
        public void onTracksChanged(Tracks tracks) {
            // getAudioFormat() aún es null aquí: el formato sale de la pista elegida.
            Format f = selectedAudioFormat(tracks);
            if (f == null) {
                return;
            }
            int bits = trackBits(f);
            String text = getString(R.string.track_format, f.sampleMimeType,
                    bits > 0 ? String.valueOf(bits) : "?", f.sampleRate, f.channelCount);
            status.setText(text);
            Log.i(TAG, text);
        }

        @Override
        public void onPlaybackStateChanged(int state) {
            if (state == Player.STATE_ENDED) {
                showResult();
            }
        }

        @Override
        public void onPlayerError(PlaybackException error) {
            // El mensaje de la excepción puede llevar la URL con la firma: no se enseña.
            String reason = error.getErrorCodeName();
            if (error.getCause() instanceof HttpDataSource.InvalidResponseCodeException) {
                reason = "HTTP " + ((HttpDataSource.InvalidResponseCodeException) error.getCause()).responseCode;
            }
            status.setText(getString(R.string.error, reason));
            Log.w(TAG, "error de reproducción: " + reason);
        }
    }

    private void showResult() {
        int bits = trackBits(player.getAudioFormat());
        String md5 = bits == 16 || bits == 24
                ? sink.md5(bits)
                : "16 bits: " + sink.md5(16) + "\n24 bits: " + sink.md5(24);
        String text = getString(R.string.result, md5, sink.samples(), sink.encodingName());
        if (sink.interrupted()) {
            text += "\n" + getString(R.string.interrupted);
        }
        result.setText(text);
        Log.i(TAG, "md5 de lo que suena=" + md5.replace('\n', ' ') + " bits=" + bits
                + " muestras=" + sink.samples() + " formato=" + sink.encodingName()
                + " salto=" + sink.interrupted());
    }
}
