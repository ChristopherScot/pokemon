-- SweepTrainers filters on last_seen and there was no index for it, so
-- every lobby load sequentially scanned the whole trainers table - work
-- that grows with the name namespace and is paid by every polling
-- client, forever, whether or not anything is actually sweepable.
--
-- Measured at 200k trainers, steady state with nothing to delete:
-- a sequential scan at 66ms becomes an index scan at 22ms. The win is
-- larger as the table grows, because the scan was the part that scaled
-- and the index lookup is not.
CREATE INDEX IF NOT EXISTS trainers_last_seen_idx ON trainers (last_seen);
