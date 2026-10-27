package io.github.mex040708afk.bitabit.desktop;

import static io.github.mex040708afk.bitabit.desktop.SessionStoreTest.TOKEN;
import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertThrows;
import static org.junit.Assert.assertTrue;

import io.github.mex040708afk.bitabit.core.BitabitClient;
import io.github.mex040708afk.bitabit.core.BitabitClient.AlbumSort;
import io.github.mex040708afk.bitabit.core.Contract.Album;
import io.github.mex040708afk.bitabit.core.Contract.AlbumSummary;
import io.github.mex040708afk.bitabit.core.Contract.Artist;
import io.github.mex040708afk.bitabit.core.Contract.ArtistSummary;
import io.github.mex040708afk.bitabit.core.Contract.SearchResult;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.List;
import java.util.Optional;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;

/** El catálogo contra un servidor simulado que responde como dice api/openapi.json. */
public class CatalogTest {

    private static final String ALBUM = "{\"id\":7,\"title\":\"\",\"artist\":{\"id\":3,\"name\":\"\"},"
            + "\"year\":null,\"trackCount\":1,\"durationMs\":215999,\"addedAt\":\"2026-10-22T10:00:00Z\","
            + "\"hasCover\":false}";

    private FakeServer server;
    private Catalog catalog;

    @Before
    public void setUp() throws IOException {
        server = new FakeServer();
        BitabitClient client = new BitabitClient(server.address());
        client.useToken(TOKEN);
        catalog = new Catalog(client, server.address());
    }

    @After
    public void tearDown() {
        server.close();
    }

    @Test
    public void loadsArtistsIncludingTheUnnamedOne() throws IOException {
        server.reply("GET /api/v1/artists", FakeServer.Reply.json(200,
                "{\"items\":[{\"id\":3,\"name\":\"\",\"albumCount\":2},{\"id\":4,\"name\":\"Miles\",\"albumCount\":1}]}"));

        List<ArtistSummary> artists = catalog.artists();

        assertEquals(2, artists.size());
        assertEquals("", artists.get(0).getName());
        assertEquals(1, artists.get(1).getAlbumCount());
        assertEquals(Optional.of("Bearer " + TOKEN), server.received().get(0).authorization());
    }

    @Test
    public void loadsAnArtistWithItsAlbums() throws IOException {
        server.reply("GET /api/v1/artists/3", FakeServer.Reply.json(200,
                "{\"id\":3,\"name\":\"\",\"albums\":[" + ALBUM + "]}"));

        Artist artist = catalog.artist(3);

        assertEquals(1, artist.getAlbums().size());
        assertFalse(artist.getAlbums().get(0).hasCover());
    }

    @Test
    public void loadsAlbums() throws IOException {
        server.reply("GET /api/v1/albums", FakeServer.Reply.json(200, "{\"items\":[" + ALBUM + "]}"));

        List<AlbumSummary> albums = catalog.albums(AlbumSort.NAME);

        assertEquals("", albums.get(0).getTitle());
    }

    @Test
    public void loadsAnAlbumWithItsTracks() throws IOException {
        server.reply("GET /api/v1/albums/7", FakeServer.Reply.json(200,
                "{\"id\":7,\"title\":\"A\",\"tracks\":[{\"id\":1,\"title\":\"T\",\"trackNumber\":1,"
                        + "\"durationMs\":1000,\"files\":[]}]}"));

        Album album = catalog.album(7);

        assertEquals(1, album.getTracks().size());
    }

    @Test
    public void loadsACoverAndReportsMissingOnes() throws IOException {
        server.reply("GET /api/v1/albums/7/cover", FakeServer.Reply.json(200, "imagen"));

        assertArrayEquals("imagen".getBytes(StandardCharsets.UTF_8), catalog.cover(7).orElseThrow());
        assertTrue(catalog.cover(8).isEmpty());
    }

    @Test
    public void searchesWithTheTextAndTheLimit() throws IOException {
        server.reply("GET /api/v1/search", FakeServer.Reply.json(200,
                "{\"artists\":[{\"id\":4,\"name\":\"Miles\",\"albumCount\":1}],\"albums\":[" + ALBUM + "],"
                        + "\"tracks\":[{\"id\":1,\"title\":\"T\",\"artist\":\"Miles\",\"album\":{\"id\":7,\"title\":\"\"},"
                        + "\"durationMs\":1000,\"files\":[]}]}"));

        SearchResult result = catalog.search("miles dávis");

        assertEquals(1, result.getArtists().size());
        assertEquals(1, result.getAlbums().size());
        assertEquals(7, result.getTracks().get(0).getAlbum().getId());
        FakeServer.Received request = server.received().get(0);
        assertEquals(Optional.of("q=miles dávis&limit=" + Catalog.SEARCH_LIMIT), request.query());
        assertEquals(Optional.of("Bearer " + TOKEN), request.authorization());
    }

    @Test
    public void aSearchWithoutOneOfItsListsIsAnError() {
        server.reply("GET /api/v1/search", FakeServer.Reply.json(200, "{\"artists\":[],\"albums\":[]}"));

        IOException e = assertThrows(IOException.class, () -> catalog.search("x"));

        assertTrue(e.getMessage(), e.getMessage().startsWith("No se pudo cargar la búsqueda."));
        assertTrue(e.getMessage(), e.getMessage().contains("pistas encontradas"));
    }

    @Test
    public void anExpiredSessionWhileSearching() {
        server.reply("GET /api/v1/search", FakeServer.Reply.json(401,
                "{\"error\":\"unauthorized\",\"message\":\"token no válido\"}"));

        assertThrows(Catalog.SessionExpired.class, () -> catalog.search("x"));
    }

    @Test
    public void whatToDoWithTheTypedText() {
        assertEquals(new Catalog.Query.Empty(), Catalog.query(""));
        assertEquals(new Catalog.Query.Empty(), Catalog.query("   \t"));
        assertEquals(new Catalog.Query.Empty(), Catalog.query(null));
        assertEquals(new Catalog.Query.Text("beatles"), Catalog.query("  beatles "));
        assertEquals(new Catalog.Query.Text("a".repeat(100)), Catalog.query(" " + "a".repeat(100) + " "));
        assertEquals(new Catalog.Query.TooLong(101), Catalog.query("a".repeat(101)));
        // Se cuentan caracteres, no unidades de UTF-16: 100 emojis caben.
        assertEquals(new Catalog.Query.Text("🎵".repeat(100)), Catalog.query("🎵".repeat(100)));
    }

    @Test
    public void unauthorizedMeansTheSessionExpired() {
        server.reply("GET /api/v1/artists", FakeServer.Reply.json(401,
                "{\"error\":\"unauthorized\",\"message\":\"token no válido\"}"));

        assertThrows(Catalog.SessionExpired.class, () -> catalog.artists());
    }

    @Test
    public void serverErrorsKeepTheContext() {
        server.reply("GET /api/v1/albums", FakeServer.Reply.json(500, "{\"error\":\"internal\",\"message\":\"x\"}"));

        IOException e = assertThrows(IOException.class, () -> catalog.albums(AlbumSort.ADDED));

        assertFalse(e instanceof Catalog.SessionExpired);
        assertTrue(e.getMessage(), e.getMessage().startsWith("No se pudo cargar los álbumes."));
        assertTrue(e.getMessage(), e.getMessage().contains("500"));
        assertTrue(e.getCause() != null);
    }

    @Test
    public void notFoundOnAnAlbumIsAnError() {
        IOException e = assertThrows(IOException.class, () -> catalog.album(99));

        assertTrue(e.getMessage(), e.getMessage().contains("404"));
    }

    @Test
    public void aResponseWithoutItsListIsAnError() {
        server.reply("GET /api/v1/artists", FakeServer.Reply.json(200, "{}"));

        IOException e = assertThrows(IOException.class, () -> catalog.artists());

        assertTrue(e.getMessage(), e.getMessage().contains("lista de artistas"));
    }

    @Test
    public void unreachableServerIsAnErrorWithContext() throws IOException {
        server.close();

        IOException e = assertThrows(IOException.class, () -> catalog.artists());

        assertTrue(e.getMessage(), e.getMessage().startsWith("No se pudo cargar los artistas."));
    }
}
