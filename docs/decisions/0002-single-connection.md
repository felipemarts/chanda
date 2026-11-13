# 0002 One SQLite connection

The store uses `SetMaxOpenConns(1)`. SQLite allows one writer; a single
connection serializes reads and writes, removes SQLITE_BUSY handling and keeps
operation order deterministic (principle 11). Cost: reads do not run in
parallel. If the latency target (p95 < 150 ms at 100k records) is missed
because of contention, add a separate read-only pool; measure first.
