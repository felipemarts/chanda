# 0070 Learned ranking: choices where BASE.md is silent

`internal/rank` implements "Ranking that learns from use". Points BASE.md
leaves open, and how they were settled:

- **Exposure (`n_shown`)** counts deliveries in retrievals that received an
  outcome. Retrievals without an outcome carry no signal, and counting them
  would need a hook in the read path; this also lets `RecomputeUtility`
  rebuild the table exactly. "Five exposures" therefore means five evaluated
  deliveries.
- **Centring.** `centred = value − moving average` with the average taken
  before the outcome counts (starts at 0, EMA with `moving_average_alpha`).
  `centred > 0` is a success, `< 0` a failure, `= 0` moves neither side.
  The stage-2 target ("used and the task succeeded") uses the same test.
- **Credit.** Only used ids that were delivered in that retrieval get credit;
  others are reported in `ignored` and change nothing (no row, no penalty).
- **One outcome per retrieval** (unique index). A repeat is `conflict`, so an
  agent cannot pump credit; the per-namespace rate limit counts stored
  outcomes in the last minute of `Clock` time.
- **External zone.** An outcome from the external zone returns a notice and
  stores nothing.
- **Flag** adds `ranking.flag_penalty` (default 10) to beta. An `active` fact
  becomes `flagged`; other fact statuses are already out of current reads and
  are kept. Events (immutable) and pages (no such status) stay as they are:
  reads exclude every flagged id through the `flags` table
  (`rank.ExcludeFlaggedSQL`, `FlaggedIDs`).
- **Reset watermark.** `ranking_reset` deletes utility and models and stamps
  `rank_state.reset_at`; recompute and training ignore signals up to it, so a
  later sleep does not bring the cleared state back. Outcomes, flags and
  retrievals are kept as the signal log; flagged records stay out of reads.
- **Toggle.** Learning is on only when `ranking.enabled` and the per-namespace
  runtime toggle are both on. Turning it off sets λ to 0; turning it back on
  needs a new promotion.
- **λ term.** Step 7 uses `(1−λ)·relevance + λ·learned`, where `learned` is
  the promoted model's score; stage-1 utility enters through the model's
  features. Without a promoted model λ is 0. `Final` returns relevance
  unchanged when λ = 0, so the order with learning off is exactly C4.
- **Promotion** evaluates the newest non-retired model. A pass needs
  `learned − baseline ≥ promotion_margin` and `learned > baseline`; a new model
  starts at one `lambda_step`. A failed gate sets λ to 0 and no model is live.
- **Features** add `rrf` to the BASE.md list (it is the relevance when no
  reranker runs). Training replays signals chronologically, so features are
  those that existed at retrieval time. Retrievals whose `params` carry
  `"gate": true` never enter training. The newest `holdout_fraction` of
  retrievals is held out; `eval_score` and `baseline_score` are held-out AUCs
  of the model and of relevance alone.
- **No randomness.** Logistic regression starts from zero; the MLP starts from
  a SHA-256 stream seeded by `ranking.model_seed`; full-batch gradient descent
  with a fixed iteration count.
