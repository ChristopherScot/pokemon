-- Game state: trainers and battles.
--
-- Unlike the reference queries, these run on the request path. Every
-- one of them is what makes two replicas safe - the guarantees are in
-- the SQL rather than in a mutex the other pod cannot see.

-- name: RegisterTrainer :one
-- Claims a name and returns the token.
--
-- The race this settles: two clients registering "ash" at the same
-- moment. lower(name) is UNIQUE, so exactly one INSERT succeeds and
-- the other gets a constraint violation the caller turns into
-- errNameTaken. A read-then-write check would let both through.
INSERT INTO trainers (token, name) VALUES ($1, $2)
RETURNING token, name, created_at, last_seen;

-- name: TrainerByToken :one
SELECT token, name, created_at, last_seen FROM trainers WHERE token = $1;

-- name: TouchTrainer :exec
-- Records that a token was used, which is what SweepTrainers reads.
-- Separate from TrainerByToken so a read stays a read.
UPDATE trainers SET last_seen = now() WHERE token = $1;

-- name: SweepTrainers :exec
-- Drops trainers nobody has been in a long time, so a name someone
-- registered once and abandoned can be claimed again. Names are unique
-- and were never released, so without this every name is spent the
-- moment it is typed - including by whoever loses their token.
--
-- NOT IN a battle, whatever last_seen says. A trainer waiting in the
-- lobby for an opponent may sit for hours without making a request,
-- and deleting them would cascade their side away and strand the other
-- player mid-game. Battles are swept on their own TTL first, so a
-- trainer only becomes sweepable once their battles have gone.
--
-- Called on write, like SweepBattles: no background goroutine to
-- supervise, and no timer in each replica racing the others.
DELETE FROM trainers
WHERE last_seen < $1
  AND token NOT IN (SELECT trainer_token FROM battle_sides);

-- name: CreateBattle :exec
INSERT INTO battles (
    id, status, version, turn, turn_number, winner, state
) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetBattle :one
SELECT id, status, version, turn, turn_number, winner,
       created_at, touched_at, state
FROM battles WHERE id = $1;

-- name: GetBattleForUpdate :one
-- The read half of update(). Inside a SERIALIZABLE transaction this
-- is what Postgres tracks to detect a conflicting write: two pods
-- reading the same row and both writing it means one gets 40001 and
-- retries, rather than silently clobbering the other.
SELECT id, status, version, turn, turn_number, winner,
       created_at, touched_at, state
FROM battles WHERE id = $1;

-- name: UpdateBattle :exec
-- version is bumped here rather than by the caller, so no path can
-- write a battle and forget to move it. Clients poll and compare
-- version, so one that does not change reads as "nothing happened".
UPDATE battles SET
    status = $2,
    version = version + 1,
    turn = $3,
    turn_number = $4,
    winner = $5,
    state = $6,
    touched_at = now()
WHERE id = $1;

-- name: ListWaitingBattles :many
-- The lobby: open invitations, newest first.
SELECT id, status, version, turn, turn_number, winner,
       created_at, touched_at, state
FROM battles
WHERE status = 'waiting'
ORDER BY created_at DESC;

-- name: SweepBattles :exec
-- Drops battles nobody has touched inside the TTL.
--
-- Called on write rather than from a timer: there is no background
-- goroutine to supervise, a store that is never written does not
-- grow, and with several replicas a timer in each would mean several
-- sweeps racing. battle_sides goes with it by ON DELETE CASCADE.
DELETE FROM battles WHERE touched_at < $1;

-- name: AddBattleSide :exec
INSERT INTO battle_sides (battle_id, idx, trainer_token)
VALUES ($1, $2, $3)
ON CONFLICT (battle_id, idx) DO UPDATE SET
    trainer_token = EXCLUDED.trainer_token;

-- name: BattleSidesFor :many
SELECT battle_id, idx, trainer_token
FROM battle_sides WHERE battle_id = $1 ORDER BY idx;

-- name: ActiveBattleForTrainer :one
-- Which battle a trainer is in, for a client that reconnects holding
-- only its token. The reason battle_sides is a table rather than part
-- of the JSONB blob: this is the one question asked across battles,
-- and scanning every state document to answer it would not scale past
-- a handful.
SELECT b.id, b.status, b.version, b.turn, b.turn_number, b.winner,
       b.created_at, b.touched_at, b.state
FROM battles b
JOIN battle_sides s ON s.battle_id = b.id
WHERE s.trainer_token = $1 AND b.status <> 'finished'
ORDER BY b.touched_at DESC
LIMIT 1;
