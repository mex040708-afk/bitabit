package io.github.mex040708afk.bitabit.core;

import java.time.Instant;
import java.util.List;
import java.util.Optional;

/**
 * Los objetos de la API v1 tal como los describe {@code api/openapi.json}.
 *
 * <p>Gson los rellena campo a campo por el nombre JSON. Los campos que el contrato
 * puede dejar en {@code null} se leen como {@link Optional}, y las listas llegan
 * siempre, aunque vacías. Las fechas son texto RFC 3339 en UTC; los métodos
 * {@code ...Instant()} las convierten.
 */
public final class Contract {

    private Contract() {
    }

    /** Datos del servidor, sin sesión: {@code GET /api/v1/info}. */
    public static final class Info {
        private String service;
        private String apiVersion;
        private String serverVersion;

        public String getService() {
            return service;
        }

        public String getApiVersion() {
            return apiVersion;
        }

        public String getServerVersion() {
            return serverVersion;
        }
    }

    /** Un dispositivo con sesión abierta. */
    public static class Session {
        private long id;
        private String device;
        private String createdAt;
        private String lastUsedAt;
        private boolean current;

        public long getId() {
            return id;
        }

        public String getDevice() {
            return device;
        }

        public Instant getCreatedAt() {
            return instant(createdAt);
        }

        public Instant getLastUsedAt() {
            return instant(lastUsedAt);
        }

        /** Si es la sesión del token de esta petición. */
        public boolean isCurrent() {
            return current;
        }
    }

    /** Respuesta de {@code POST /api/v1/sessions}: el token, que se muestra una sola vez. */
    public static final class Login {
        private String token;
        private Session session;

        public String getToken() {
            return token;
        }

        public Session getSession() {
            return session;
        }
    }

    /** Dispositivos con sesión abierta: {@code GET /api/v1/sessions}. */
    public static final class SessionList {
        private List<Session> items;

        public List<Session> getItems() {
            return items;
        }
    }

    /** Un artista dentro de otro objeto. Vacío si las pistas no dicen el artista. */
    public static final class ArtistRef {
        private long id;
        private String name;

        public long getId() {
            return id;
        }

        public String getName() {
            return name;
        }
    }

    /** Un álbum dentro de otro objeto. Vacío si las pistas no dicen el álbum. */
    public static final class AlbumRef {
        private long id;
        private String title;

        public long getId() {
            return id;
        }

        public String getTitle() {
            return title;
        }
    }

    /** Un artista de álbum y cuántos álbumes tiene. */
    public static class ArtistSummary {
        private long id;
        private String name;
        private int albumCount;

        public long getId() {
            return id;
        }

        public String getName() {
            return name;
        }

        public int getAlbumCount() {
            return albumCount;
        }
    }

    /** Lista de artistas: {@code GET /api/v1/artists}. */
    public static final class ArtistList {
        private List<ArtistSummary> items;

        public List<ArtistSummary> getItems() {
            return items;
        }
    }

    /** Un artista con sus álbumes: {@code GET /api/v1/artists/{id}}. */
    public static final class Artist {
        private long id;
        private String name;
        private List<AlbumSummary> albums;

        public long getId() {
            return id;
        }

        public String getName() {
            return name;
        }

        public List<AlbumSummary> getAlbums() {
            return albums;
        }
    }

    /** Un álbum sin sus pistas. */
    public static class AlbumSummary {
        private long id;
        private String title;
        private ArtistRef artist;
        private Integer year;
        private int trackCount;
        private long durationMs;
        private String addedAt;
        private boolean hasCover;

        public long getId() {
            return id;
        }

        public String getTitle() {
            return title;
        }

        public ArtistRef getArtist() {
            return artist;
        }

        /** El año más antiguo de las fechas de sus pistas; vacío si ninguna trae fecha. */
        public Optional<Integer> getYear() {
            return Optional.ofNullable(year);
        }

        public int getTrackCount() {
            return trackCount;
        }

        public long getDurationMs() {
            return durationMs;
        }

        /** Cuándo se importó su última pista. */
        public Instant getAddedAt() {
            return instant(addedAt);
        }

        /** Si tiene carátula: {@code GET /api/v1/albums/{id}/cover} la sirve. */
        public boolean hasCover() {
            return hasCover;
        }
    }

    /** Lista de álbumes: {@code GET /api/v1/albums}. */
    public static final class AlbumList {
        private List<AlbumSummary> items;

        public List<AlbumSummary> getItems() {
            return items;
        }
    }

    /** Un álbum con sus pistas: {@code GET /api/v1/albums/{id}}. */
    public static final class Album extends AlbumSummary {
        private List<Track> tracks;

        public List<Track> getTracks() {
            return tracks;
        }
    }

    /** Una versión de una pista: la original o la AAC de 256 kbit/s. */
    public static final class TrackFile {
        private String quality;
        private String codec;
        private boolean lossless;
        private Integer bits;
        private int sampleRate;
        private int channels;
        private long bytes;

        public String getQuality() {
            return quality;
        }

        public String getCodec() {
            return codec;
        }

        public boolean isLossless() {
            return lossless;
        }

        /** Bits por muestra; vacío si la versión tiene pérdida. */
        public Optional<Integer> getBits() {
            return Optional.ofNullable(bits);
        }

        public int getSampleRate() {
            return sampleRate;
        }

        public int getChannels() {
            return channels;
        }

        public long getBytes() {
            return bytes;
        }
    }

    /** Una pista con sus versiones: {@code GET /api/v1/tracks/{id}}. */
    public static final class Track {
        private long id;
        private String title;
        private String artist;
        private AlbumRef album;
        private ArtistRef albumArtist;
        private Integer discNumber;
        private Integer trackNumber;
        private long durationMs;
        private List<TrackFile> files;

        public long getId() {
            return id;
        }

        public String getTitle() {
            return title;
        }

        /** El artista de esta pista tal como viene en la etiqueta. */
        public String getArtist() {
            return artist;
        }

        public AlbumRef getAlbum() {
            return album;
        }

        public ArtistRef getAlbumArtist() {
            return albumArtist;
        }

        public Optional<Integer> getDiscNumber() {
            return Optional.ofNullable(discNumber);
        }

        public Optional<Integer> getTrackNumber() {
            return Optional.ofNullable(trackNumber);
        }

        public long getDurationMs() {
            return durationMs;
        }

        /** Las versiones que se pueden pedir, primero la original. */
        public List<TrackFile> getFiles() {
            return files;
        }
    }

    /** Un enlace firmado para escuchar una versión: {@code GET /api/v1/tracks/{id}/link}. */
    public static final class Link {
        private String path;
        private String quality;
        private String expiresAt;

        /** Ruta relativa al servidor ({@code /media/...}): el cliente le pone delante la dirección. */
        public String getPath() {
            return path;
        }

        public String getQuality() {
            return quality;
        }

        public Instant getExpiresAt() {
            return instant(expiresAt);
        }
    }

    /** Resultados de la búsqueda: {@code GET /api/v1/search}. */
    public static final class SearchResult {
        private List<ArtistSummary> artists;
        private List<AlbumSummary> albums;
        private List<Track> tracks;

        public List<ArtistSummary> getArtists() {
            return artists;
        }

        public List<AlbumSummary> getAlbums() {
            return albums;
        }

        public List<Track> getTracks() {
            return tracks;
        }
    }

    /**
     * Cuerpo de un error de la API. {@code attemptsLeft} solo viene en un 401 de
     * contraseña equivocada, y {@code retryAfter} solo en un 429 por bloqueo.
     */
    public static final class ErrorBody {
        private String error;
        private String message;
        private Integer attemptsLeft;
        private Integer retryAfter;

        /** El código del contrato: bad_request, unauthorized, wrong_password, blocked, not_found o internal. */
        public String getError() {
            return error;
        }

        public String getMessage() {
            return message;
        }

        public Optional<Integer> getAttemptsLeft() {
            return Optional.ofNullable(attemptsLeft);
        }

        /** Segundos que faltan para que acabe el bloqueo. */
        public Optional<Integer> getRetryAfter() {
            return Optional.ofNullable(retryAfter);
        }
    }

    /** Cuerpo de {@code POST /api/v1/sessions}. El contrato no admite más campos. */
    public static final class LoginRequest {
        private final String password;
        private final String device;

        public LoginRequest(String password, String device) {
            this.password = password;
            this.device = device;
        }
    }

    private static Instant instant(String rfc3339) {
        return rfc3339 == null ? null : Instant.parse(rfc3339);
    }
}
