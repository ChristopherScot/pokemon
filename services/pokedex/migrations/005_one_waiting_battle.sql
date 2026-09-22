-- One waiting battle per trainer, enforced where the concurrency is.
--
-- The cap was a Go check: count the trainer's open battles, then
-- insert. Two round-trips with nothing held between them, so
-- concurrent requests all read the same count and all proceed.
-- Reproduced against production: 12 parallel creates from one token
-- against a cap of 1 gave 8 successes, and that trainer then held the
-- entire lobby - exactly the abuse the cap was added to stop, just
-- committed in parallel rather than in sequence. The sequential test
-- passed throughout, because it never overlapped the window.
--
-- This is the same move RegisterTrainer already uses for names: make
-- the database refuse it, and map the violation to the 409 the
-- handler already returns. A read-then-write check lets both through;
-- a unique index lets exactly one.
--
-- Partial, on side 0 only: side 0 is the trainer who OPENED the
-- battle, and a joiner is side 1 by which point the battle is no
-- longer waiting. Keyed on the token rather than the name so a
-- reclaimed name cannot inherit the old holder's slot.
--
-- Postgres cannot put a unique index across two tables, so the
-- trainer's token is denormalised onto battles for the waiting rows
-- that need it. NULL everywhere else, and a unique index ignores
-- NULLs, so only waiting battles are constrained.
ALTER TABLE battles ADD COLUMN IF NOT EXISTS waiting_for_token text;

-- Backfill: existing waiting battles get their opener's token. If two
-- already share one - which the race allowed - only the oldest keeps
-- it, because the index below would reject the rest.
UPDATE battles b
SET waiting_for_token = s.trainer_token
FROM battle_sides s
WHERE s.battle_id = b.id
  AND s.idx = 0
  AND b.status = 'waiting'
  AND b.waiting_for_token IS NULL
  AND b.id = (
      SELECT b2.id FROM battles b2
      JOIN battle_sides s2 ON s2.battle_id = b2.id AND s2.idx = 0
      WHERE s2.trainer_token = s.trainer_token AND b2.status = 'waiting'
      ORDER BY b2.created_at
      LIMIT 1
  );

-- Anything left over is a duplicate the old race let through; it has
-- no token set, so it is unconstrained rather than deleted. Its owner
-- simply cannot open another until it expires.
CREATE UNIQUE INDEX IF NOT EXISTS battles_one_waiting_per_trainer
    ON battles (waiting_for_token)
    WHERE waiting_for_token IS NOT NULL;
