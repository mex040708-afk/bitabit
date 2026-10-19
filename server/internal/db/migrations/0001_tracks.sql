-- Pistas importadas y sus versiones (original y AAC 256).
-- En la fase 1 las etiquetas van como texto; artists y albums llegan en la fase 2.
-- Ninguna ruta se guarda: se deduce del ID (biblioteca/orig/{id}.{ext}).

CREATE TABLE tracks (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title         text        NOT NULL CHECK (title <> ''),
    artist        text        NOT NULL DEFAULT '',
    album         text        NOT NULL DEFAULT '',
    album_artist  text        NOT NULL DEFAULT '',
    release_date  text        NOT NULL DEFAULT '',
    track_number  smallint    CHECK (track_number BETWEEN 1 AND 9999),
    track_total   smallint    CHECK (track_total BETWEEN 1 AND 9999),
    disc_number   smallint    CHECK (disc_number BETWEEN 1 AND 9999),
    disc_total    smallint    CHECK (disc_total BETWEEN 1 AND 9999),
    duration_ms   integer     NOT NULL CHECK (duration_ms > 0),
    -- SHA-256 del archivo tal como llegó a entrada/: evita importarlo dos veces.
    source_sha256 bytea       NOT NULL UNIQUE CHECK (octet_length(source_sha256) = 32),
    imported_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE track_files (
    track_id    bigint   NOT NULL REFERENCES tracks (id) ON DELETE CASCADE,
    quality     text     NOT NULL CHECK (quality IN ('orig', 'aac256')),
    ext         text     NOT NULL CHECK (ext ~ '^[a-z0-9]{1,8}$'),
    codec       text     NOT NULL CHECK (codec <> ''),
    lossless    boolean  NOT NULL,
    sample_rate integer  NOT NULL CHECK (sample_rate > 0),
    bits        smallint CHECK (bits BETWEEN 1 AND 32),
    channels    smallint NOT NULL CHECK (channels > 0),
    bytes       bigint   NOT NULL CHECK (bytes > 0),
    -- SHA-256 del archivo guardado: da el ETag.
    sha256      bytea    NOT NULL CHECK (octet_length(sha256) = 32),
    -- MD5 del audio de STREAMINFO; NULL si no consta o si hay pérdida.
    audio_md5   bytea    CHECK (octet_length(audio_md5) = 16),
    PRIMARY KEY (track_id, quality),
    CHECK (lossless = (bits IS NOT NULL)),
    CHECK (audio_md5 IS NULL OR lossless),
    CHECK (quality <> 'aac256' OR (ext = 'm4a' AND codec = 'aac' AND NOT lossless))
);
