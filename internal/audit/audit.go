// Package audit writes the hash-chained audit log (principle 10).
//
// hash = SHA-256(prev_hash || "\n" || id || "\n" || ts || "\n" || namespace ||
//
//	"\n" || actor || "\n" || action || "\n" || target || "\n" || details)
//
// The first entry's prev_hash is the genesis value "0"*64.
// Details must never contain content that forget could later need to erase:
// store ids, counts, hashes of configuration — never record text.
package audit

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/store"
)

const Genesis = "0000000000000000000000000000000000000000000000000000000000000000"

// Action names. Add new ones here so the vocabulary stays documented.
const (
	ActWrite        = "write"     // event written (remember, ingest chunk, log_session turn)
	ActDuplicate    = "duplicate" // exact duplicate: no new event
	ActRetire       = "retire"    // chunk retired by a new document version
	ActFactInsert   = "fact_insert"
	ActFactConfirm  = "fact_confirm"
	ActSupersede    = "supersede"
	ActQuarantine   = "quarantine"
	ActStaleSource  = "stale_source"
	ActNearDup      = "near_duplicate"
	ActEntityCreate = "entity_create"
	ActEntityMerge  = "entity_merge"
	ActFlag         = "flag"
	ActOutcome      = "outcome"
	ActPromote      = "promote"
	ActForget       = "forget"
	ActSleepStart   = "sleep_start"
	ActSleepEnd     = "sleep_end"
	ActSleepRevert  = "sleep_revert"
	ActPageUpdate   = "page_update"
	ActRankAdjust   = "rank_adjust"
	ActRankPromote  = "rank_promote"
	ActRankReset    = "rank_reset"
	ActRankToggle   = "rank_toggle"
	ActMigrate      = "migrate"
)

type Entry struct {
	Namespace string
	Actor     string
	Action    string
	Target    string // record id or scope label
	Details   any    // marshalled to canonical JSON (encoding/json sorts map keys)
}

type Log struct {
	clock core.Clock
}

func New(clock core.Clock) *Log { return &Log{clock: clock} }

// Append writes e inside q (use the caller's *sql.Tx so the audit row
// commits atomically with the mutation). Returns the audit id.
func (l *Log) Append(ctx context.Context, q store.Queryer, e Entry) (int64, error) {
	details := "{}"
	if e.Details != nil {
		b, err := json.Marshal(e.Details)
		if err != nil {
			return 0, err
		}
		details = string(b)
	}
	var prevID int64
	prev := Genesis
	err := q.QueryRowContext(ctx, `SELECT id, hash FROM audit_log ORDER BY id DESC LIMIT 1`).Scan(&prevID, &prev)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	id := prevID + 1
	ts := core.FormatTime(l.clock.Now())
	h := Hash(prev, id, ts, e.Namespace, e.Actor, e.Action, e.Target, details)
	_, err = q.ExecContext(ctx,
		`INSERT INTO audit_log(id, ts, namespace, actor, action, target, details, prev_hash, hash)
		 VALUES (?,?,?,?,?,?,?,?,?)`, id, ts, e.Namespace, e.Actor, e.Action, e.Target, details, prev, h)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func Hash(prev string, id int64, ts, ns, actor, action, target, details string) string {
	s := strings.Join([]string{prev, strconv.FormatInt(id, 10), ts, ns, actor, action, target, details}, "\n")
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// VerifyResult reports the outcome of a chain check.
type VerifyResult struct {
	Entries  int64  `json:"entries"`
	OK       bool   `json:"ok"`
	BrokenAt int64  `json:"broken_at,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Verify walks the whole chain.
func Verify(ctx context.Context, q store.Queryer) (VerifyResult, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, ts, namespace, actor, action, target, details, prev_hash, hash FROM audit_log ORDER BY id`)
	if err != nil {
		return VerifyResult{}, err
	}
	defer rows.Close()
	res := VerifyResult{OK: true}
	prev := Genesis
	var expectID int64 = 1
	for rows.Next() {
		var id int64
		var ts, ns, actor, action, target, details, prevHash, hash string
		if err := rows.Scan(&id, &ts, &ns, &actor, &action, &target, &details, &prevHash, &hash); err != nil {
			return res, err
		}
		res.Entries++
		switch {
		case id != expectID:
			return fail(res, id, fmt.Sprintf("gap: expected id %d", expectID)), nil
		case prevHash != prev:
			return fail(res, id, "prev_hash does not match previous hash"), nil
		case Hash(prev, id, ts, ns, actor, action, target, details) != hash:
			return fail(res, id, "hash mismatch"), nil
		}
		prev = hash
		expectID++
	}
	return res, rows.Err()
}

func fail(r VerifyResult, id int64, reason string) VerifyResult {
	r.OK, r.BrokenAt, r.Reason = false, id, reason
	return r
}
