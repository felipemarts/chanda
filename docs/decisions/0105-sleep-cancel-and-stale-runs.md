# 0105 Cancelled and stale sleep runs never stay "running"

`App.Close` cancels the scheduler's context and waits. A run in progress
then failed its next query, and `finish` tried to close the row **with the
cancelled context**, so the `UPDATE sleep_runs` never happened and the row
stayed `running` forever (and its `sleep_start` had no `sleep_end`).

- `finish` writes with `context.WithoutCancel`. A run whose context was
  cancelled ends `failed`, with the notice "run cancelled before it
  finished (shutdown)…" in its report and a `sleep_end` audit. `failed`,
  not `partial`: partial means "a ceiling was reached and the rest waits",
  while a cancelled run may have stopped anywhere; whatever the gate had
  not committed was rolled back with the staging transaction.
- A process killed mid-run (no `finish` at all) leaves a `running` row.
  `Sleeper.RecoverStale` marks every row still `running` whose `started_at`
  is older than `sleep.stale_run_minutes` (default 60; 0 disables) as
  `failed`, sets `ended_at`, appends a line to its report and audits
  `sleep_recover` (run id; details: status and the timeout). It runs when
  the scheduler starts, from `App.Start` when the scheduler is off, and at
  the start of every `Run` (under the run lock, so never on a run of this
  process). The timeout protects a run of another process on the same file.
- The scheduler loop takes `Deps.Ticks` (a channel) instead of a wall-clock
  ticker when set, so tests drive `Start` tick by tick with the fake Clock;
  every decision already used the Clock.
