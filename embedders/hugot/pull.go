package hugot

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	khugot "github.com/knights-analytics/hugot"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

// Pull downloads model name from Hugging Face into dir and writes its
// manifest. It is the install step and the only code here that uses the
// network; Open and OpenReranker never do. dir must not exist yet (or hold a
// verified copy of the same model, which is kept). hugot's progress output
// goes to stderr.
func Pull(ctx context.Context, name, dir string) (Manifest, error) {
	m, ok := LookupModel(name)
	if !ok {
		return Manifest{}, core.Errf(core.CodeInvalidArgument, "hugot: unknown model %q (known: %s)", name, modelNames())
	}
	if man, err := ReadManifest(dir); err == nil && man.Model == m.Name {
		if err := VerifyManifest(dir, man); err != nil {
			return man, err
		}
		return man, nil // already installed
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		return Manifest{}, core.Errf(core.CodeConflict, "hugot: %q already exists; pull into a new directory", dir)
	}
	parent := filepath.Dir(filepath.Clean(dir))
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return Manifest{}, err
	}
	tmp, err := os.MkdirTemp(parent, ".chanda-pull-")
	if err != nil {
		return Manifest{}, err
	}
	defer os.RemoveAll(tmp)
	var got string
	err = stdoutToStderr(func() error {
		opts := khugot.NewDownloadOptions()
		opts.OnnxFilePath = m.OnnxFile
		opts.Verbose = true
		var err error
		got, err = khugot.DownloadModel(ctx, m.Repo, tmp, opts)
		return err
	})
	if err != nil {
		return Manifest{}, core.Wrap(core.CodeUnavailable, err, "hugot: download "+m.Repo)
	}
	if err := os.Rename(got, dir); err != nil {
		return Manifest{}, err
	}
	return WriteManifest(dir, m)
}
