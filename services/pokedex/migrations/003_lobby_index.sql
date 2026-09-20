-- A partial index for the lobby.
--
-- battles_waiting_idx was (status, created_at DESC), which indexes
-- every battle including the finished ones the lobby never reads.
-- Measured at 200k rows with 4k waiting: the planner chose a bitmap
-- heap scan and then an explicit sort - 17ms - and the index was
-- megabytes.
--
-- WHERE status = 'waiting' indexes only the rows the query wants, so
-- the scan is a plain index scan in index order with no sort, and the
-- index is 104 kB. With the LIMIT added to ListWaitingBattles the
-- same query measures 0.1ms.
--
-- Not CONCURRENTLY: the migration runner puts each file in a
-- transaction, and CREATE INDEX CONCURRENTLY cannot run in one. At
-- this table's size the brief lock is not worth the machinery; if
-- that changes, the runner needs to learn about non-transactional
-- migrations first.
CREATE INDEX IF NOT EXISTS battles_waiting_partial_idx
    ON battles (created_at DESC)
    WHERE status = 'waiting';

DROP INDEX IF EXISTS battles_waiting_idx;
