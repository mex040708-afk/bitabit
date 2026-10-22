-- La contraseña de la única cuenta y sus sesiones (ADR-014). No se guardan IPs
-- ni agentes de usuario: los contadores del bloqueo por IP están en memoria.

-- Una sola fila. No existe hasta que se pone la contraseña con `bitabit password`.
CREATE TABLE account (
    id            smallint    PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    -- Argon2id en formato PHC: el hash lleva sus parámetros, así que subirlos
    -- más adelante no rompe las contraseñas guardadas.
    password_hash text        NOT NULL CHECK (password_hash LIKE '$argon2id$%'),
    changed_at    timestamptz NOT NULL
);

-- Una fila por sesión abierta. El token no se guarda, solo su SHA-256.
CREATE TABLE sessions (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    token_sha256 bytea       NOT NULL UNIQUE CHECK (octet_length(token_sha256) = 32),
    device       text        NOT NULL CHECK (char_length(device) BETWEEN 1 AND 64),
    created_at   timestamptz NOT NULL,
    last_used_at timestamptz NOT NULL
);
