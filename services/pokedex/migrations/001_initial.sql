-- The pokedex schema, in two halves that behave differently.
--
-- Reference data (pokemon, moves, pokemon_moves) is read-only at
-- runtime and seeded from pokedex.json. Game state (trainers, battles)
-- is written on every request and is what multiple replicas must agree
-- about.
--
-- Applied by the service at startup, in one transaction, guarded by
-- schema_migrations. Two replicas starting together is the normal case,
-- so this has to be safe to run concurrently - see migrate.go.

CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------- reference

CREATE TABLE IF NOT EXISTS pokemon (
    id                   INTEGER PRIMARY KEY,
    name                 TEXT NOT NULL UNIQUE,
    description          TEXT NOT NULL DEFAULT '',
    genus                TEXT NOT NULL DEFAULT '',
    habitat              TEXT NOT NULL DEFAULT '',
    -- One or two entries. An array rather than a join table: nothing
    -- joins on type, and the only query is "does this Pokemon have
    -- type X", which a GIN index answers.
    types                TEXT[] NOT NULL,
    height               INTEGER NOT NULL,
    weight               INTEGER NOT NULL,
    sprite               TEXT NOT NULL,
    artwork              TEXT NOT NULL DEFAULT '',
    evolves_from         TEXT NOT NULL DEFAULT '',
    legendary            BOOLEAN NOT NULL DEFAULT false,
    -- The battle formula's inputs. Checked non-zero because a missing
    -- base stat gives every Pokemon identical HP, which is invisible
    -- until someone notices the numbers never differ.
    base_hp              INTEGER NOT NULL CHECK (base_hp > 0),
    base_attack          INTEGER NOT NULL CHECK (base_attack > 0),
    base_defense         INTEGER NOT NULL CHECK (base_defense > 0),
    base_special_attack  INTEGER NOT NULL CHECK (base_special_attack > 0),
    base_special_defense INTEGER NOT NULL CHECK (base_special_defense > 0),
    base_speed           INTEGER NOT NULL CHECK (base_speed > 0)
);

CREATE INDEX IF NOT EXISTS pokemon_types_idx ON pokemon USING GIN (types);

CREATE TABLE IF NOT EXISTS moves (
    name         TEXT PRIMARY KEY,
    type         TEXT NOT NULL,
    power        INTEGER NOT NULL DEFAULT 0,
    description  TEXT NOT NULL DEFAULT '',
    effect       TEXT NOT NULL DEFAULT '',
    -- NULL means the move cannot miss, which is not the same as 100.
    accuracy     INTEGER,
    pp           INTEGER NOT NULL DEFAULT 0,
    damage_class TEXT NOT NULL DEFAULT 'status'
);

-- Which Pokemon knows which move, and in what capacity.
--
-- One table with a `kind` rather than two: the six battle moves are a
-- subset of the learnable set, and splitting them would mean writing
-- the same pair twice.
--
-- `slot` preserves the order the upstream Pokedex returns. That order
-- is not cosmetic - a Pokemon's battle moves are the first six, and
-- sorting them would silently rewrite every moveset.
CREATE TABLE IF NOT EXISTS pokemon_moves (
    pokemon_id INTEGER NOT NULL REFERENCES pokemon(id) ON DELETE CASCADE,
    move_name  TEXT NOT NULL REFERENCES moves(name) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('battle', 'learnable')),
    slot       INTEGER NOT NULL,
    PRIMARY KEY (pokemon_id, move_name, kind)
);

CREATE INDEX IF NOT EXISTS pokemon_moves_lookup_idx
    ON pokemon_moves (pokemon_id, kind, slot);

-- ---------------------------------------------------------------- game state

-- A trainer is a name and the token that proves you are them.
--
-- The token is the primary key because every authenticated request
-- arrives holding one and nothing else. name is UNIQUE because
-- registration races: two clients claiming "ash" at once used to be
-- settled by a mutex in one process, and is now settled here, which is
-- the only place that still works with two replicas.
CREATE TABLE IF NOT EXISTS trainers (
    token      TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Case-insensitive, matching the in-memory store's strings.ToLower.
-- "Ash" and "ash" are the same claim.
CREATE UNIQUE INDEX IF NOT EXISTS trainers_name_key ON trainers (lower(name));

-- A battle: the columns anything queries or orders by, plus the rest as
-- one JSONB document.
--
-- Combatants, stat stages and the event log live in `state` rather than
-- their own tables because nothing reads them piecemeal - a battle is
-- always fetched whole by id, and a turn rewrites most of it. One row
-- UPDATE is also exactly the unit SERIALIZABLE needs to detect two
-- players moving at once.
--
-- What is NOT in the blob: status and touched_at, because the lobby
-- and the sweep query them; version, because it is the conflict check.
CREATE TABLE IF NOT EXISTS battles (
    id          TEXT PRIMARY KEY,
    status      TEXT NOT NULL,
    version     INTEGER NOT NULL DEFAULT 0,
    turn        INTEGER NOT NULL DEFAULT 0,
    turn_number INTEGER NOT NULL DEFAULT 0,
    winner      TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    touched_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    state       JSONB NOT NULL
);

-- The lobby lists waiting battles newest first; the sweep deletes by
-- touched_at. Both are covered here.
CREATE INDEX IF NOT EXISTS battles_waiting_idx
    ON battles (status, created_at DESC);
CREATE INDEX IF NOT EXISTS battles_touched_idx ON battles (touched_at);

-- Who is on which side, as rows rather than inside the blob.
--
-- This is the one part of a battle that is queried across battles:
-- "which battle is this trainer in" is how a reconnecting client finds
-- its game, and a JSONB scan of every row is the wrong way to answer
-- it.
CREATE TABLE IF NOT EXISTS battle_sides (
    battle_id     TEXT NOT NULL REFERENCES battles(id) ON DELETE CASCADE,
    idx           INTEGER NOT NULL,
    trainer_token TEXT NOT NULL REFERENCES trainers(token) ON DELETE CASCADE,
    PRIMARY KEY (battle_id, idx)
);

CREATE INDEX IF NOT EXISTS battle_sides_trainer_idx
    ON battle_sides (trainer_token);
