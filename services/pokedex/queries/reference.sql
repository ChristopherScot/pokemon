-- Reference data: the Pokedex itself.
--
-- Read at startup and held in memory. It is 100 Pokemon and 562 moves
-- that never change between deploys, so querying per request would buy
-- nothing and put the database on the path of every /pokemon call.
-- The database is the source of truth; this is a cache of it that
-- lives as long as the process.

-- name: ListPokemon :many
SELECT * FROM pokemon ORDER BY id;

-- name: ListMoves :many
SELECT * FROM moves ORDER BY name;

-- name: ListPokemonMoves :many
-- Every Pokemon-to-move link, both kinds, in the order the upstream
-- Pokedex gave them. Loaded whole and grouped in Go rather than joined
-- per Pokemon: it is one query instead of a hundred.
SELECT pokemon_id, move_name, kind, slot
FROM pokemon_moves
ORDER BY pokemon_id, kind, slot;

-- name: CountPokemon :one
SELECT count(*) FROM pokemon;

-- Seeding. Run once from pokedex.json when the tables are empty.
--
-- ON CONFLICT DO UPDATE rather than DO NOTHING: re-seeding after the
-- data file changes should update what is there, and a seed that
-- silently skips existing rows would leave the database on an old
-- version of the Pokedex with no sign of it.

-- name: UpsertPokemon :exec
INSERT INTO pokemon (
    id, name, description, genus, habitat, types, height, weight,
    sprite, artwork, evolves_from, legendary,
    base_hp, base_attack, base_defense,
    base_special_attack, base_special_defense, base_speed
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
    $13, $14, $15, $16, $17, $18
)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    description = EXCLUDED.description,
    genus = EXCLUDED.genus,
    habitat = EXCLUDED.habitat,
    types = EXCLUDED.types,
    height = EXCLUDED.height,
    weight = EXCLUDED.weight,
    sprite = EXCLUDED.sprite,
    artwork = EXCLUDED.artwork,
    evolves_from = EXCLUDED.evolves_from,
    legendary = EXCLUDED.legendary,
    base_hp = EXCLUDED.base_hp,
    base_attack = EXCLUDED.base_attack,
    base_defense = EXCLUDED.base_defense,
    base_special_attack = EXCLUDED.base_special_attack,
    base_special_defense = EXCLUDED.base_special_defense,
    base_speed = EXCLUDED.base_speed;

-- name: UpsertMove :exec
INSERT INTO moves (
    name, type, power, description, effect, accuracy, pp, damage_class
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (name) DO UPDATE SET
    type = EXCLUDED.type,
    power = EXCLUDED.power,
    description = EXCLUDED.description,
    effect = EXCLUDED.effect,
    accuracy = EXCLUDED.accuracy,
    pp = EXCLUDED.pp,
    damage_class = EXCLUDED.damage_class;

-- name: UpsertPokemonMove :exec
INSERT INTO pokemon_moves (pokemon_id, move_name, kind, slot)
VALUES ($1, $2, $3, $4)
ON CONFLICT (pokemon_id, move_name, kind) DO UPDATE SET
    slot = EXCLUDED.slot;

-- name: DeletePokemonMovesFor :exec
-- Clears a Pokemon's links before re-seeding them, so a move dropped
-- upstream does not linger. Scoped to one Pokemon rather than
-- truncating the table, which would break the foreign key for every
-- other row mid-seed.
DELETE FROM pokemon_moves WHERE pokemon_id = $1;
