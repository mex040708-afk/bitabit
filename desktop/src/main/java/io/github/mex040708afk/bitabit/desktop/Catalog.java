package io.github.mex040708afk.bitabit.desktop;

import io.github.mex040708afk.bitabit.core.ApiException;
import io.github.mex040708afk.bitabit.core.BitabitClient;
import io.github.mex040708afk.bitabit.core.BitabitClient.AlbumSort;
import io.github.mex040708afk.bitabit.core.Contract.Album;
import io.github.mex040708afk.bitabit.core.Contract.AlbumSummary;
import io.github.mex040708afk.bitabit.core.Contract.Artist;
import io.github.mex040708afk.bitabit.core.Contract.ArtistSummary;
import io.github.mex040708afk.bitabit.core.Contract.SearchResult;
import java.io.IOException;
import java.util.List;
import java.util.Optional;

/**
 * El catálogo del servidor (artistas, álbumes, pistas y carátulas), sin nada de interfaz.
 * Todas las llamadas esperan a la red: se hacen desde un hilo de fondo (ADR-026).
 *
 * <p>Cada error sale como {@link IOException} con un texto para enseñar que dice qué se
 * intentaba cargar; un 401 sale como {@link SessionExpired}, para que la app vuelva al login.
 */
final class Catalog {

    /** El servidor ya no acepta el token de la sesión. */
    static final class SessionExpired extends IOException {

        private static final long serialVersionUID = 1L;

        SessionExpired(ApiException cause) {
            super("La sesión ha caducado.", cause);
        }
    }

    /** Resultados de cada tipo que se piden en una búsqueda: los que da la API por defecto. */
    static final int SEARCH_LIMIT = 20;
    /** Lo más largo que admite la API, en caracteres, ya quitados los espacios de los extremos. */
    static final int MAX_QUERY_CHARS = 100;

    /** Lo que se hace con el texto del campo de búsqueda. */
    sealed interface Query {

        /** No queda nada que buscar: se limpian los resultados. */
        record Empty() implements Query {
        }

        /** Más largo de lo que admite la API: se avisa sin llamar al servidor. */
        record TooLong(int chars) implements Query {
        }

        record Text(String text) implements Query {
        }
    }

    /** Decide qué hacer con lo que se ha escrito, con las mismas reglas que core y la API. */
    static Query query(String typed) {
        String text = typed == null ? "" : typed.trim();
        int chars = text.codePointCount(0, text.length());
        if (chars == 0) {
            return new Query.Empty();
        }
        return chars > MAX_QUERY_CHARS ? new Query.TooLong(chars) : new Query.Text(text);
    }

    @FunctionalInterface
    private interface Call<T> {
        T run() throws IOException;
    }

    private final BitabitClient client;
    private final String server;

    Catalog(BitabitClient client, String server) {
        this.client = client;
        this.server = server;
    }

    List<ArtistSummary> artists() throws IOException {
        return call("los artistas", () -> required(client.artists(), "la lista de artistas"));
    }

    /** Un artista con sus álbumes. */
    Artist artist(long artistId) throws IOException {
        return call("el artista", () -> {
            Artist artist = client.artist(artistId);
            required(artist.getAlbums(), "los álbumes del artista");
            return artist;
        });
    }

    List<AlbumSummary> albums(AlbumSort sort) throws IOException {
        return call("los álbumes", () -> required(client.albums(sort), "la lista de álbumes"));
    }

    /** Un álbum con sus pistas. */
    Album album(long albumId) throws IOException {
        return call("el álbum", () -> {
            Album album = client.album(albumId);
            required(album.getTracks(), "las pistas del álbum");
            return album;
        });
    }

    /** Artistas, álbumes y pistas parecidos a {@code text}, que ya viene de {@link #query}. */
    SearchResult search(String text) throws IOException {
        return call("la búsqueda", () -> {
            SearchResult result = client.search(text, SEARCH_LIMIT);
            required(result.getArtists(), "los artistas encontrados");
            required(result.getAlbums(), "los álbumes encontrados");
            required(result.getTracks(), "las pistas encontradas");
            return result;
        });
    }

    /**
     * La carátula tal como la guarda el servidor. Core aún no pide {@code ?size=256}
     * (API 1.1.0): la vista la reduce al decodificarla.
     */
    Optional<byte[]> cover(long albumId) throws IOException {
        return call("la carátula", () -> client.cover(albumId));
    }

    private <T> T call(String what, Call<T> call) throws IOException {
        try {
            return call.run();
        } catch (ApiException e) {
            if (e.status() == 401) {
                throw new SessionExpired(e);
            }
            throw new IOException("No se pudo cargar " + what + ". " + Messages.request(server, e), e);
        } catch (IOException e) {
            throw new IOException("No se pudo cargar " + what + ". " + Messages.request(server, e), e);
        }
    }

    /** Un servidor que omite una lista del contrato responde algo que no se puede pintar. */
    private static <T> T required(T value, String what) throws IOException {
        if (value == null) {
            throw new IOException("la respuesta no trae " + what);
        }
        return value;
    }
}
