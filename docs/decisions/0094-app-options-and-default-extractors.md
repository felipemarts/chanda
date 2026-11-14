# 0094 App options (C1..C5) and the default extractor set

## Options

`app.NewWith(env, app.Options{...})` and `app.OpenWith(ctx, cfg, opts)`
select the parts of the system an App runs; the zero value is the full
system (`app.New`, `app.Open`).

| Field | Effect |
|---|---|
| `DisableVectors` | read path keyword only (no query embedding, no vector list); embeddings are still queued |
| `DisableFacts` | no extraction on write; `assert_fact` answers `unavailable`; sleep extracts nothing |
| `DisableSleep` | `sleep_run` answers `unavailable`; no scheduler |
| `DisableLearning` | λ stays 0, no utility or shadow scoring in reads, no training or promotion in sleep; outcomes and flags are still stored |
| `Embedder` | replaces the configured embedder |
| `Reranker` | cross-encoder of read step 6, used when `read.rerank_enabled` is true |
| `LLM` | replaces the configured sleep provider (tests pass `llm.Fake`) |
| `Logf` | background errors |

The evaluation configurations:

    C1  Options{DisableFacts: true, DisableSleep: true, DisableLearning: true}
    C2  C1 + Reranker (read.rerank_enabled: true)
    C3  Options{Reranker: rr, DisableSleep: true, DisableLearning: true}
    C4  Options{Reranker: rr, DisableLearning: true}
    C5  Options{Reranker: rr}

## Default extractors

`write.extractors` defaults to `[{name: header, kind: header}]`: the
`Key: Value` lines at the top of a chunk (after its headings) or a
front-matter block become facts whose subject is a `title`/`name`/`subject`
key, else the nearest heading, else the last element of the chunk's heading
path; the predicate is the normalized key. A document `# Project Atlas` with
`Owner: Marina Duarte` therefore answers `timeline(Project Atlas, owner)`.
A chunk with no heading and no title key yields nothing. `extractors: []`
in the configuration turns every deterministic extractor off.
