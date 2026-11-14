package sleep

import "context"

// phase2 is LLM consolidation. Without a provider it only leaves a notice.
func (s *Sleeper) phase2(ctx context.Context, r *run) error {
	if s.d.LLM == nil {
		r.res.notice("phase 2 skipped: no LLM provider configured; phase 1 only")
		return nil
	}
	r.res.notice("phase 2 not implemented yet")
	return nil
}
