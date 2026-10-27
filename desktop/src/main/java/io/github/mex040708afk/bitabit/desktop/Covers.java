package io.github.mex040708afk.bitabit.desktop;

import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.function.Consumer;
import javafx.scene.image.Image;

/**
 * Las carátulas de los álbumes: se piden al servidor fuera del hilo de JavaFX, se reducen
 * al decodificarlas y se guardan en una caché con tope (ADR-026). Solo se usa desde el hilo
 * de JavaFX.
 */
final class Covers {

    static final int CACHE_SIZE = 200;
    /** Lado, en píxeles, al que se reduce lo que llega: sobra para la rejilla y para la cabecera del álbum. */
    static final int DECODED_SIZE = 240;

    private static final System.Logger LOG = System.getLogger(Covers.class.getName());

    private final Catalog catalog;
    private final Background background;
    private final LruCache<Long, Image> cache = new LruCache<>(CACHE_SIZE);
    /** Quién espera cada carátula que ya se está pidiendo, para no pedirla dos veces. */
    private final Map<Long, List<Consumer<Optional<Image>>>> waiting = new HashMap<>();

    Covers(Catalog catalog, Background background) {
        this.catalog = catalog;
        this.background = background;
    }

    /**
     * Pide la carátula del álbum; {@code done} recibe la imagen, o vacío si no se pudo
     * cargar (el motivo queda en el registro). Si ya está en la caché, responde enseguida.
     */
    void request(long albumId, Consumer<Optional<Image>> done) {
        Optional<Image> cached = cache.get(albumId);
        if (cached.isPresent()) {
            done.accept(cached);
            return;
        }
        List<Consumer<Optional<Image>>> others = waiting.get(albumId);
        if (others != null) {
            others.add(done);
            return;
        }
        List<Consumer<Optional<Image>>> list = new ArrayList<>();
        list.add(done);
        waiting.put(albumId, list);
        background.run(() -> load(albumId), image -> finish(albumId, image), e -> {
            LOG.log(System.Logger.Level.WARNING, "No se pudo cargar la carátula del álbum " + albumId, e);
            finish(albumId, Optional.empty());
        });
    }

    private void finish(long albumId, Optional<Image> image) {
        image.ifPresent(i -> cache.put(albumId, i));
        for (Consumer<Optional<Image>> done : waiting.remove(albumId)) {
            done.accept(image);
        }
    }

    /** Decodificar aquí, en el hilo de fondo, es lo que evita que la ventana se pare con una carátula grande. */
    private Optional<Image> load(long albumId) throws IOException {
        Optional<byte[]> bytes = catalog.cover(albumId);
        if (bytes.isEmpty()) {
            return Optional.empty();
        }
        Image image = new Image(new ByteArrayInputStream(bytes.get()), DECODED_SIZE, DECODED_SIZE, true, true);
        if (image.isError()) {
            throw new IOException("la carátula del álbum " + albumId + " no es una imagen que se pueda leer",
                    image.getException());
        }
        return Optional.of(image);
    }
}
