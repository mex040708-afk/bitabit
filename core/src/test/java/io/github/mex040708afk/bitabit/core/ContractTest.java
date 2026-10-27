package io.github.mex040708afk.bitabit.core;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import io.github.mex040708afk.bitabit.core.Contract.Album;
import io.github.mex040708afk.bitabit.core.Contract.AlbumSummary;
import io.github.mex040708afk.bitabit.core.Contract.ErrorBody;
import io.github.mex040708afk.bitabit.core.Contract.Info;
import io.github.mex040708afk.bitabit.core.Contract.Link;
import io.github.mex040708afk.bitabit.core.Contract.Login;
import io.github.mex040708afk.bitabit.core.Contract.SearchResult;
import io.github.mex040708afk.bitabit.core.Contract.Track;
import java.time.Instant;
import java.util.List;
import org.junit.Test;

/**
 * Lectura de los cuerpos del contrato. Los JSON son los que manda el servidor, con
 * los mismos nombres de campo que {@code api/openapi.json}.
 */
public class ContractTest {

    @Test
    public void readsInfo() {
        Info info = Json.fromJson("{\"service\":\"bitabit\",\"apiVersion\":\"1.0.0\",\"serverVersion\":\"0.2.0\"}",
                Info.class);
        assertEquals("bitabit", info.getService());
        assertEquals("1.0.0", info.getApiVersion());
        assertEquals("0.2.0", info.getServerVersion());
    }

    @Test
    public void readsLoginAndItsSession() {
        Login login = Json.fromJson("{\"token\":\"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8\","
                + "\"session\":{\"id\":7,\"device\":\"Pixel 7a\",\"createdAt\":\"2026-10-22T08:00:00.123456Z\","
                + "\"lastUsedAt\":\"2026-10-22T08:00:00Z\",\"current\":true}}", Login.class);
        assertEquals(43, login.getToken().length());
        assertEquals(7, login.getSession().getId());
        assertEquals("Pixel 7a", login.getSession().getDevice());
        assertTrue(login.getSession().isCurrent());
        assertEquals(Instant.parse("2026-10-22T08:00:00Z"), login.getSession().getLastUsedAt());
    }

    @Test
    public void albumWithoutDatesHasNoYear() {
        AlbumSummary album = Json.fromJson("{\"id\":3,\"title\":\"Sin fecha\","
                + "\"artist\":{\"id\":1,\"name\":\"Grupo\"},\"year\":null,\"trackCount\":2,\"durationMs\":240000,"
                + "\"addedAt\":\"2026-10-21T10:00:00Z\",\"hasCover\":false}", AlbumSummary.class);
        assertFalse(album.getYear().isPresent());
        assertEquals(2, album.getTrackCount());
        assertFalse(album.hasCover());
    }

    @Test
    public void albumWithDatesHasItsYear() {
        AlbumSummary album = Json.fromJson("{\"id\":4,\"title\":\"Disco\",\"artist\":{\"id\":1,\"name\":\"Grupo\"},"
                + "\"year\":1975,\"trackCount\":3,\"durationMs\":360000,\"addedAt\":\"2026-10-21T10:00:00Z\","
                + "\"hasCover\":true}", AlbumSummary.class);
        assertEquals(Integer.valueOf(1975), album.getYear().orElseThrow());
        assertTrue(album.hasCover());
    }

    @Test
    public void lossyTrackFileHasNoBitsAndLosslessOneDoes() {
        Track track = Json.fromJson("{\"id\":9,\"title\":\"Uno\",\"artist\":\"Grupo\","
                + "\"album\":{\"id\":4,\"title\":\"Disco\"},\"albumArtist\":{\"id\":1,\"name\":\"Grupo\"},"
                + "\"discNumber\":null,\"trackNumber\":1,\"durationMs\":120000,\"files\":["
                + "{\"quality\":\"orig\",\"codec\":\"flac\",\"lossless\":true,\"bits\":16,\"sampleRate\":44100,"
                + "\"channels\":2,\"bytes\":1000},"
                + "{\"quality\":\"aac256\",\"codec\":\"aac\",\"lossless\":false,\"bits\":null,\"sampleRate\":44100,"
                + "\"channels\":2,\"bytes\":500}]}", Track.class);
        assertFalse(track.getDiscNumber().isPresent());
        assertEquals(Integer.valueOf(1), track.getTrackNumber().orElseThrow());
        assertEquals(2, track.getFiles().size());
        assertEquals(Integer.valueOf(16), track.getFiles().get(0).getBits().orElseThrow());
        assertFalse(track.getFiles().get(1).getBits().isPresent());
    }

    @Test
    public void albumDetailKeepsItsTracks() {
        Album album = Json.fromJson("{\"id\":4,\"title\":\"Disco\",\"artist\":{\"id\":1,\"name\":\"Grupo\"},"
                + "\"year\":null,\"trackCount\":1,\"durationMs\":60000,\"addedAt\":\"2026-10-21T10:00:00Z\","
                + "\"hasCover\":false,\"tracks\":[]}", Album.class);
        assertEquals("Disco", album.getTitle());
        assertTrue(album.getTracks().isEmpty());
    }

    @Test
    public void emptySearchStillHasItsLists() {
        SearchResult result = Json.fromJson("{\"artists\":[],\"albums\":[],\"tracks\":[]}", SearchResult.class);
        assertTrue(result.getArtists().isEmpty());
        assertTrue(result.getAlbums().isEmpty());
        assertTrue(result.getTracks().isEmpty());
    }

    @Test
    public void readsLinkAndItsExpiry() {
        Link link = Json.fromJson("{\"path\":\"/media/4312.flac?q=orig&exp=1767225600&kid=k1&sig=abc\","
                + "\"quality\":\"orig\",\"expiresAt\":\"2026-01-01T12:00:00Z\"}", Link.class);
        assertTrue(link.getPath().startsWith("/media/"));
        assertEquals(Instant.parse("2026-01-01T12:00:00Z"), link.getExpiresAt());
    }

    @Test
    public void wrongPasswordCarriesAttemptsLeft() {
        ErrorBody error = Json.fromJson("{\"error\":\"wrong_password\",\"message\":\"Contraseña equivocada.\","
                + "\"attemptsLeft\":9}", ErrorBody.class);
        assertEquals("wrong_password", error.getError());
        assertEquals(Integer.valueOf(9), error.getAttemptsLeft().orElseThrow());
        assertFalse(error.getRetryAfter().isPresent());
    }

    @Test
    public void blockedCarriesRetryAfter() {
        ErrorBody error = Json.fromJson("{\"error\":\"blocked\",\"message\":\"Demasiados fallos desde esta IP.\","
                + "\"retryAfter\":86400}", ErrorBody.class);
        assertEquals("blocked", error.getError());
        assertEquals(Integer.valueOf(86400), error.getRetryAfter().orElseThrow());
        assertFalse(error.getAttemptsLeft().isPresent());
    }

    @Test
    public void unknownFieldsAreIgnored() {
        Info info = Json.fromJson("{\"service\":\"bitabit\",\"apiVersion\":\"1.1.0\",\"serverVersion\":\"0.3.0\","
                + "\"campoNuevo\":{\"anidado\":true}}", Info.class);
        assertEquals("1.1.0", info.getApiVersion());
    }

    @Test
    public void listsOfAnEmptyLibraryAreEmptyNotNull() {
        List<AlbumSummary> items = Json.fromJson("{\"items\":[]}", Contract.AlbumList.class).getItems();
        assertTrue(items.isEmpty());
    }

    @Test(expected = IllegalArgumentException.class)
    public void emptyBodyIsAnError() {
        Json.fromJson("", Info.class);
    }
}
