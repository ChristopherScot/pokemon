-- Constrain the columns whose values are a fixed set.
--
-- battles.status is compared against exact spellings by the lobby
-- query, the join guard and the sweep. A typo in a future write path
-- would produce a battle that is invisible to the lobby and immortal
-- to the sweep, and nothing would have objected. The Go side now uses
-- the generated api.BattleStatus constants, so this closes the one
-- remaining door: a string arriving from outside that code path.
--
-- NOT VALID then VALIDATE is the online-safe two-step. Adding a
-- validated CHECK takes ACCESS EXCLUSIVE and scans the table; adding
-- it NOT VALID takes only a brief lock, and VALIDATE then scans under
-- SHARE UPDATE EXCLUSIVE, which does not block reads or writes.
ALTER TABLE battles
    ADD CONSTRAINT battles_status_check
    CHECK (status IN ('waiting', 'active', 'finished')) NOT VALID;

ALTER TABLE battles VALIDATE CONSTRAINT battles_status_check;

-- Same reasoning for the move catalogue, which is written only by
-- seeding today but is a free-text column that the damage formula
-- switches on.
ALTER TABLE moves
    ADD CONSTRAINT moves_damage_class_check
    CHECK (damage_class IN ('physical', 'special', 'status')) NOT VALID;

ALTER TABLE moves VALIDATE CONSTRAINT moves_damage_class_check;
