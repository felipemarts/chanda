# 0106 Header and table extractors fall back to the file name

Decision 0094 left a chunk with no heading and no title key without facts:
a plain `notes/project-atlas.md` starting with `Owner: Marina Duarte` gave
nothing.

The subject order of the `header` and `kv_table` extractors is now: the
configured subject, a `title`/`name`/`subject` key (header only), the
nearest heading, the last element of the heading path, and finally **the
document's file name without its extension** (`project-atlas`; a name
without an extension is used whole). The name is taken as is, not
re-cased or de-hyphenated, so it is what the user sees in the folder.

`facts.Event` gained `DocURI`; `internal/app` fills it from
`documents.uri` for document chunks. Sessions and statements have no
document and keep yielding nothing without a heading or title key.
