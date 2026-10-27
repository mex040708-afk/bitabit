package io.github.mex040708afk.bitabit.desktop;

import io.github.mex040708afk.bitabit.core.ApiException;
import io.github.mex040708afk.bitabit.core.BitabitClient;
import io.github.mex040708afk.bitabit.core.BitabitClient.AlbumSort;
import io.github.mex040708afk.bitabit.core.BitabitClient.Quality;
import io.github.mex040708afk.bitabit.core.Contract.Album;
import io.github.mex040708afk.bitabit.core.Contract.AlbumSummary;
import io.github.mex040708afk.bitabit.core.Contract.Artist;
import io.github.mex040708afk.bitabit.core.Contract.ArtistSummary;
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

    /**
     * La URL firmada de una versión de la pista, pedida justo antes de cargarla porque caduca.
     * Lleva la firma: no se enseña ni se registra.
     */
    String link(long trackId, Quality quality) throws IOException {
        return call("el enlace de la pista", () -> {
            String path = required(client.link(trackId, quality).getPath(), "la ruta del enlace");
            if (!path.startsWith("/media/")) {
                throw new IOException("el enlace no es una ruta /media/");
            }
            return server + path;
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
