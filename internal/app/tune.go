package app

import (
	"context"
	"fmt"

	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/env"
)

// tuneStore sizes the connection's page cache and memory map from
// store.cache_mb and store.mmap_mb (decision 0100). SQLite's default 2 MB
// cache turns every read of a 100k-record database into pread syscalls.
// The store has a single connection that is never recycled
// (SetConnMaxLifetime(0), one idle connection), so a PRAGMA set here holds
// for the life of the App. Neither changes the file's content.
func tuneStore(e *env.Env) error {
	ctx := context.Background()
	db := e.Store.DB()
	if mb := e.Cfg.Store.CacheMB; mb > 0 {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA cache_size = %d", -mb*1024)); err != nil {
			return core.Wrap(core.CodeInternal, err, "store.cache_mb")
		}
	}
	if mb := e.Cfg.Store.MmapMB; mb > 0 {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA mmap_size = %d", int64(mb)<<20)); err != nil {
			return core.Wrap(core.CodeInternal, err, "store.mmap_mb")
		}
	}
	return nil
}
