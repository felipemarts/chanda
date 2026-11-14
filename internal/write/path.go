package write

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

// PathResult reports a folder (or single file) ingest.
type PathResult struct {
	SourceURI string         `json:"source_uri"`
	Documents []IngestResult `json:"documents"` // in walk order
	Created   int            `json:"created"`   // documents seen for the first time
	Updated   int            `json:"updated"`   // documents with a new version
	Unchanged int            `json:"unchanged"`
	Rejected  int            `json:"rejected"`
}

// IngestPath ingests a file or a folder as documents of one source (kind
// document, zone internal). The walk is deterministic: entries in lexical
// order, hidden files and folders (".name") skipped, symlinks not followed,
// only extensions in write.ingest_extensions. Each file is one document whose
// uri is sourceURI + "/" + its slash-separated path relative to the root
// (for a single file, its base name). sourceURI defaults to the cleaned path.
//
// Each document is written in its own transaction. A document refused by
// admission is reported and the walk continues; any other error stops it and
// returns the results so far.
func (w *Writer) IngestPath(ctx context.Context, ns, path, sourceURI, actor string) (PathResult, error) {
	return w.IngestPathWith(ctx, PathInput{Namespace: ns, Path: path, SourceURI: sourceURI, Actor: actor})
}

// PathInput is IngestPath with every option.
type PathInput struct {
	Namespace  string
	Path       string
	SourceURI  string
	SourceKind core.SourceKind // empty: document (internal); web for fetched pages (external)
	Actor      string
	MaxZone    core.Zone // channel ceiling (decision 0090); empty: none
}

// IngestPathWith is IngestPath with a source kind and a zone ceiling.
func (w *Writer) IngestPathWith(ctx context.Context, in PathInput) (PathResult, error) {
	path, sourceURI, actor := in.Path, in.SourceURI, in.Actor
	if err := checkActor(actor); err != nil {
		return PathResult{}, err
	}
	if sourceURI == "" {
		sourceURI = filepath.ToSlash(filepath.Clean(path))
	}
	sourceURI = strings.TrimSuffix(sourceURI, "/")
	res := PathResult{SourceURI: sourceURI}

	files, err := w.listFiles(path)
	if err != nil {
		return res, err
	}
	for _, f := range files {
		r, err := w.ingestFile(ctx, in, f.abs, sourceURI+"/"+f.rel, sourceURI)
		if err != nil && core.CodeOf(err) != core.CodeRejected {
			return res, err
		}
		res.Documents = append(res.Documents, r)
		switch r.Action {
		case ActionCreated:
			res.Created++
		case ActionUpdated:
			res.Updated++
		case ActionUnchanged:
			res.Unchanged++
		case ActionRejected:
			res.Rejected++
		}
	}
	return res, nil
}

type fileEntry struct{ abs, rel string }

func (w *Writer) listFiles(root string) ([]fileEntry, error) {
	info, err := os.Stat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, core.Errf(core.CodeNotFound, "path %s not found", root)
	}
	if err != nil {
		return nil, core.Wrap(core.CodeInvalidArgument, err, "ingest path")
	}
	exts := map[string]bool{}
	for _, e := range w.e.Cfg.Write.IngestExtensions {
		exts[strings.ToLower(e)] = true
	}
	if !info.IsDir() {
		return []fileEntry{{root, filepath.Base(root)}}, nil
	}
	var out []fileEntry
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !exts[strings.ToLower(filepath.Ext(d.Name()))] {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, fileEntry{p, filepath.ToSlash(rel)})
		return nil
	})
	if err != nil {
		return nil, core.Wrap(core.CodeInvalidArgument, err, "walk "+root)
	}
	return out, nil
}

func (w *Writer) ingestFile(ctx context.Context, in PathInput, abs, uri, sourceURI string) (IngestResult, error) {
	// A file larger than 4 bytes per allowed rune cannot pass the size rule: skip reading it.
	if max := w.e.Cfg.Write.MaxChars; max > 0 {
		if fi, err := os.Stat(abs); err == nil && fi.Size() > 4*int64(max) {
			return IngestResult{URI: uri, Action: ActionRejected, Reason: ReasonTooLong}, rejected(ReasonTooLong)
		}
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return IngestResult{URI: uri}, core.Wrap(core.CodeInvalidArgument, err, "read "+uri)
	}
	return w.IngestDocument(ctx, IngestInput{Namespace: in.Namespace, URI: uri, Content: string(b), SourceURI: sourceURI,
		SourceKind: in.SourceKind, Actor: in.Actor, MaxZone: in.MaxZone})
}
