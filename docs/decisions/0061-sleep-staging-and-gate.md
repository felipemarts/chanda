# 0061 Sleep staging, snapshot and gate revert

BASE.md: phase-2 changes "stay staged until they pass the gate"; a drop above
the tolerance "discards the staged changes and restores the snapshot".

**Choice: the staging area is an uncommitted SQLite savepoint.**

1. All LLM calls happen first, outside any transaction; they only build an
   in-memory plan. The single connection is never held during network I/O.
2. `applyGated` takes the single connection (`db.Conn`), runs
   `VACUUM INTO {snapshot_dir}/{run}.db` and `BEGIN` on it. With
   `MaxOpenConns(1)` nothing can interleave, so the snapshot is exactly the
   state the transaction starts from.
3. Inside the transaction: evaluator score and integrity count (before),
   `SAVEPOINT sleep_stage`, the plan is written (facts through `FactSink`,
   verdicts, page items with status `staged`), staged items are promoted so
   the gate measures them, score and integrity count again (after).
4. Drop > `sleep.gate_tolerance` points, or more integrity violations:
   `ROLLBACK TO sleep_stage`. Otherwise `RELEASE`. Then `COMMIT`.

**Why not restore the snapshot file.** Swapping the file would need closing
the store under every other component, would drop writes that arrived after
the snapshot, and would replace `audit_log` with an older copy (losing
committed audit rows). The rollback yields the same state as the snapshot
(proved by a test that compares the derived tables with the snapshot file)
without any of that. The snapshot file stays as a disaster-recovery copy and
is never read by the revert, so a snapshot deleted by `forget` mid-run cannot
break a revert (the run then records no snapshot path and adds a notice).

**Audit chain.** Staged mutations append audit rows inside the savepoint.
On revert they are rolled back with the data — they were never committed,
so the committed chain never contained them and no append-only trigger
fires (a rollback is not a DELETE). `ActSleepRevert` is then appended in the
same transaction; `audit.Append` reads the last row through the tx, so it
chains to the last committed hash and `audit.Verify` passes. Its details
hold scores, tolerance, violation counts, the snapshot path and counts of
what was discarded (no content). The committed history of a reverted run is
`sleep_start`, `sleep_revert`, `sleep_end`.

**Consequences.**

- `Evaluator.Score` and `Integrity.Check` receive a `store.Queryer` (the tx)
  and must use only it; with one connection, using `Store.DB()` there
  deadlocks. The evaluator must be read-only.
- Reads and writes wait while the gate runs (CPU only, no LLM). Expected
  seconds for a 50–100 question gate set.
- Vectors of records staged in this run are computed after commit, so the
  gate sees them through keyword search only.
- A crash before `COMMIT` leaves nothing staged behind.
- After a revert the events stay unprocessed, so the next run retries them.
- Each candidate runs in its own nested savepoint; a sink error coded
  `rejected`/`invalid_argument` drops only that candidate.
