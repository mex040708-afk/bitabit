-- Búsqueda y orden sin distinguir tildes, con tolerancia a erratas (ADR-007 y
-- ADR-014). Las extensiones van en public: son de toda la base, y así los tests,
-- que trabajan en esquemas temporales, las comparten.

CREATE EXTENSION IF NOT EXISTS unaccent SCHEMA public;
CREATE EXTENSION IF NOT EXISTS pg_trgm SCHEMA public;

-- unaccent no es IMMUTABLE, y las columnas generadas y los índices lo necesitan.
-- Esta envoltura sí lo es: el diccionario no cambia mientras la base exista.
CREATE FUNCTION immutable_unaccent(text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT
    AS $$ SELECT public.unaccent('public.unaccent', $1) $$;

-- Búsqueda por palabras (tsvector, con prefijos) y por trigramas (erratas). Las
-- columnas se recalculan solas cuando cambian el nombre o el título.
ALTER TABLE artists ADD COLUMN search tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', immutable_unaccent(name))) STORED;
ALTER TABLE albums ADD COLUMN search tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', immutable_unaccent(title))) STORED;
ALTER TABLE tracks ADD COLUMN search tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', immutable_unaccent(title))) STORED;

CREATE INDEX artists_search ON artists USING gin (search);
CREATE INDEX albums_search ON albums USING gin (search);
CREATE INDEX tracks_search ON tracks USING gin (search);
CREATE INDEX artists_trgm ON artists USING gin (immutable_unaccent(name) public.gin_trgm_ops);
CREATE INDEX albums_trgm ON albums USING gin (immutable_unaccent(title) public.gin_trgm_ops);
CREATE INDEX tracks_trgm ON tracks USING gin (immutable_unaccent(title) public.gin_trgm_ops);
