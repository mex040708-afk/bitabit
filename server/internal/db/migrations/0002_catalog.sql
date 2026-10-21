-- Artistas y álbumes sacados de las etiquetas, y la versión de la biblioteca
-- que da el ETag del catálogo (ADR-014).
-- Artista de álbum = album_artist o, si está vacío, artist. Álbum = (artista de
-- álbum, título). Los nombres se comparan exactos.

CREATE TABLE artists (
    id   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text   NOT NULL UNIQUE
);

CREATE TABLE albums (
    id        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    artist_id bigint NOT NULL REFERENCES artists (id),
    title     text   NOT NULL,
    -- Carátula en biblioteca/covers/{id}.{cover_ext}; NULL si no tiene.
    cover_ext text   CHECK (cover_ext IN ('jpg', 'png')),
    UNIQUE (artist_id, title)
);

-- Una sola fila. Sube con cada importación, en la misma transacción.
CREATE TABLE library (
    id      smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    version bigint   NOT NULL DEFAULT 1 CHECK (version > 0)
);
INSERT INTO library DEFAULT VALUES;

-- Lo ya importado: cada pista a su álbum.
INSERT INTO artists (name)
SELECT DISTINCT COALESCE(NULLIF(album_artist, ''), artist) FROM tracks;

INSERT INTO albums (artist_id, title)
SELECT DISTINCT ar.id, t.album
FROM tracks t
JOIN artists ar ON ar.name = COALESCE(NULLIF(t.album_artist, ''), t.artist);

ALTER TABLE tracks ADD COLUMN album_id bigint REFERENCES albums (id);

UPDATE tracks t
SET album_id = al.id
FROM albums al
JOIN artists ar ON ar.id = al.artist_id
WHERE ar.name = COALESCE(NULLIF(t.album_artist, ''), t.artist)
  AND al.title = t.album;

ALTER TABLE tracks ALTER COLUMN album_id SET NOT NULL;
CREATE INDEX tracks_album_id ON tracks (album_id);
