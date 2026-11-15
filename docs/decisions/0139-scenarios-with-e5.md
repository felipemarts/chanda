# 0139 Phase 5 scenarios with e5 through eval/lme

The phase 5 scenarios (`eval/scenarios`) ran only with the built-in hash
embedder: 12 of 15 passed, I1, A1 and A2 failing on what the pre-context
delivers for requests the corpus does not answer. Whether e5-small changes
that was unmeasured, and the machine holds one e5 process at a time, which
the test's own `chanda serve` processes (up to four at once) cannot each
load.

## Mechanism

`go test ./eval/scenarios/ -run TestScenarios -args -lme LME -embed-url URL
[-update]` (also `make scenarios-e5 [EMBED_URL=…] [SCEN_ARGS=-update]`):

- LME is `eval/lme/bin/lme`, built from the same tree (`lme chanda …` is
  `internal/cli.Run` with the `lme-cached` provider registered); every
  `chanda serve` and every person-side CLI call of the scenarios runs as
  `lme chanda …` instead of the binary the test builds;
- the scenario configuration adds `embed.provider: lme-cached`,
  `model_id: e5-small@hugot0.8.6+simd`, `dim: 384`; the provider embeds
  through the embed server at URL (`LME_EMBED_INNER=e5`, no client-side
  cache, so both runs embed through the server) and refuses a server that
  serves another model;
- results go to `results-e5.json` and `results-e5.md`, with the server and
  embedder named in their header; the check, determinism and `-strict`
  logic is the same as the default run.

Without `-lme` nothing changes: the default run builds `./cmd/chanda`,
writes the same configuration and compares with the same
`results.json`/`results.md`, which are byte-identical to the committed
ones (checked with `go test ./eval/scenarios/` after the change).

## Result (e5-small, two invocations, each running the set twice)

| id | hash (results.md) | e5 (results-e5.md) |
| --- | --- | --- |
| F1 F2 F3 F4 | PASS | PASS |
| U1 | PASS | PASS |
| I1 | FAIL (3 blocks, 2 expected) | FAIL (3 blocks, 2 expected) |
| I2 I3 I4 | PASS | PASS |
| T1 T2 | PASS | PASS |
| D1 D2 | PASS | PASS |
| A1 | FAIL ("receita de bolo de cenoura…" gets the refund statement) | **PASS** |
| A2 | FAIL (all three near-topic queries filled) | FAIL (all three filled) |
| total | 12 of 15 | **13 of 15** |

Both e5 invocations were byte-identical run to run and to the committed
files. With e5, the off-topic query of A1 no longer reaches the e5 cosine
floor (0.8161, decision 0124), so A1 passes; the near-topic queries of A2
("Quem aprova os deploys no cluster de staging em Frankfurt?", "Qual e o
horario de limpeza da maquina de lavar louca?", "Qual o telefone do suporte
da VPN?") share words with the corpus and still pass the coverage floor
(0.25), the case decision 0124 and decision 0140 describe; I1's third block
is the same request the internal turns do not answer. The embed server
caches vectors on its side, so the second invocation's determinism covers
Chanda's processing of identical vectors, not the model's numerics.
