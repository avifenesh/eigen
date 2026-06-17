package memory

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go sqlite (no cgo; works with CGO_ENABLED=0)
)

// Index is the memory bookkeeping store (~/.eigen/memory/index.sqlite): it
// tracks per-session rollout summaries (which session, where its raw file is,
// how often its knowledge gets used) and the background job queue that drives
// generation (stage1 → consolidate → summary → forget). Mirrors codex's
// memories_1.sqlite. Pure-Go sqlite, so it ships in the static binary.
//
// The index is advisory: the source of truth is the markdown tiers on disk
// (raw/MEMORY.md/SUMMARY.md). Losing the index only loses usage stats + job
// watermarks; a rebuild re-derives them from the raw/ dir.
type Index struct {
	mu sync.Mutex
	db *sql.DB
}

// IndexPath is ~/.eigen/memory/index.sqlite.
func IndexPath() (string, error) {
	base, err := baseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "index.sqlite"), nil
}

// OpenIndex opens (creating + migrating) the memory index.
func OpenIndex() (*Index, error) {
	p, err := IndexPath()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+p+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // serialize: a single local writer, avoids "database is locked"
	idx := &Index{db: db}
	if err := idx.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return idx, nil
}

func (i *Index) Close() error {
	if i == nil || i.db == nil {
		return nil
	}
	return i.db.Close()
}

func (i *Index) migrate() error {
	_, err := i.db.Exec(`
CREATE TABLE IF NOT EXISTS summaries (
  scope        TEXT NOT NULL,   -- memory scope key (project dir hash, or "global")
  session_id   TEXT NOT NULL,   -- source session id
  slug         TEXT NOT NULL,   -- short slug used in the raw filename
  raw_path     TEXT NOT NULL,   -- path to raw/<ts>-<slug>.md
  outcome      TEXT,            -- success | partial | failed
  watermark    INTEGER,         -- source transcript mtime/size signature when summarized
  generated_at INTEGER NOT NULL,
  usage_count  INTEGER NOT NULL DEFAULT 0,
  last_used    INTEGER,
  in_summary   INTEGER NOT NULL DEFAULT 1, -- whether it currently feeds SUMMARY.md
  PRIMARY KEY (scope, session_id)
);
CREATE TABLE IF NOT EXISTS jobs (
  kind          TEXT NOT NULL,  -- mem_stage1 | mem_consolidate | mem_summary | mem_forget
  scope         TEXT NOT NULL,
  job_key       TEXT NOT NULL,  -- dedup key (e.g. session id for stage1)
  status        TEXT NOT NULL,  -- pending | running | done | error
  lease_until   INTEGER,        -- a worker holds this job until this unix time
  retry_remaining INTEGER NOT NULL DEFAULT 2,
  last_error    TEXT,
  updated_at    INTEGER NOT NULL,
  PRIMARY KEY (kind, scope, job_key)
);`)
	return err
}

// --- summaries ---------------------------------------------------------------

// SummaryRow is one per-session rollout summary's bookkeeping.
type SummaryRow struct {
	Scope, SessionID, Slug, RawPath, Outcome string
	Watermark, GeneratedAt, UsageCount       int64
	LastUsed                                 int64
	InSummary                                bool
}

// RecordSummary upserts a per-session summary row (called after a raw rollout
// summary is written).
func (i *Index) RecordSummary(r SummaryRow) error {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	_, err := i.db.Exec(`
INSERT INTO summaries (scope, session_id, slug, raw_path, outcome, watermark, generated_at, usage_count, last_used, in_summary)
VALUES (?,?,?,?,?,?,?,COALESCE((SELECT usage_count FROM summaries WHERE scope=? AND session_id=?),0),?,1)
ON CONFLICT(scope, session_id) DO UPDATE SET
  slug=excluded.slug, raw_path=excluded.raw_path, outcome=excluded.outcome,
  watermark=excluded.watermark, generated_at=excluded.generated_at`,
		r.Scope, r.SessionID, r.Slug, r.RawPath, r.Outcome, r.Watermark, r.GeneratedAt,
		r.Scope, r.SessionID, r.LastUsed)
	return err
}

// Summarized reports whether a session is already summarized at the given
// watermark (so stage1 can skip unchanged sessions — idempotency).
func (i *Index) Summarized(scope, sessionID string, watermark int64) bool {
	if i == nil {
		return false
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	var wm int64
	err := i.db.QueryRow(`SELECT watermark FROM summaries WHERE scope=? AND session_id=?`, scope, sessionID).Scan(&wm)
	return err == nil && wm == watermark && watermark != 0
}

// BumpUsage increments usage for the summaries whose knowledge was used (by
// session id), updating last_used — the forgetting signal.
func (i *Index) BumpUsage(scope string, sessionIDs ...string) {
	if i == nil || len(sessionIDs) == 0 {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	now := time.Now().Unix()
	for _, id := range sessionIDs {
		_, _ = i.db.Exec(`UPDATE summaries SET usage_count=usage_count+1, last_used=? WHERE scope=? AND session_id=?`, now, scope, id)
	}
}

// Summaries lists a scope's summary rows, newest first.
func (i *Index) Summaries(scope string) ([]SummaryRow, error) {
	if i == nil {
		return nil, nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	rows, err := i.db.Query(`SELECT scope,session_id,slug,raw_path,COALESCE(outcome,''),COALESCE(watermark,0),generated_at,usage_count,COALESCE(last_used,0),in_summary FROM summaries WHERE scope=? ORDER BY generated_at DESC`, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SummaryRow
	for rows.Next() {
		var r SummaryRow
		var in int
		if err := rows.Scan(&r.Scope, &r.SessionID, &r.Slug, &r.RawPath, &r.Outcome, &r.Watermark, &r.GeneratedAt, &r.UsageCount, &r.LastUsed, &in); err != nil {
			return nil, err
		}
		r.InSummary = in != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- jobs (leased queue) -----------------------------------------------------

// Enqueue adds (or resets) a pending job, deduped by (kind, scope, job_key).
func (i *Index) Enqueue(kind, scope, jobKey string) error {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	_, err := i.db.Exec(`
INSERT INTO jobs (kind, scope, job_key, status, retry_remaining, updated_at)
VALUES (?,?,?, 'pending', 2, ?)
ON CONFLICT(kind, scope, job_key) DO UPDATE SET
  status=CASE WHEN jobs.status IN ('done','error') THEN 'pending' ELSE jobs.status END,
  updated_at=excluded.updated_at`,
		kind, scope, jobKey, time.Now().Unix())
	return err
}

// Job is a queued memory job.
type Job struct {
	Kind, Scope, JobKey, Status, LastError string
	RetryRemaining                         int
}

// Claim leases one pending (or lease-expired running) job for leaseSecs,
// marking it running. Returns ok=false when the queue is empty.
func (i *Index) Claim(leaseSecs int64) (Job, bool, error) {
	if i == nil {
		return Job{}, false, nil
	}
	return i.claim("", leaseSecs)
}

// ClaimScope leases one pending job for scope only. This lets a per-scope
// Pipeline drain its own queue without stealing work for another project.
func (i *Index) ClaimScope(scope string, leaseSecs int64) (Job, bool, error) {
	if i == nil {
		return Job{}, false, nil
	}
	return i.claim(scope, leaseSecs)
}

func (i *Index) claim(scope string, leaseSecs int64) (Job, bool, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	now := time.Now().Unix()
	var j Job
	where := "WHERE status='pending' OR (status='running' AND COALESCE(lease_until,0) < ?)"
	args := []any{now}
	if scope != "" {
		where = "WHERE scope=? AND (status='pending' OR (status='running' AND COALESCE(lease_until,0) < ?))"
		args = []any{scope, now}
	}
	err := i.db.QueryRow(`
SELECT kind,scope,job_key,status,COALESCE(last_error,''),retry_remaining FROM jobs
`+where+`
ORDER BY
  CASE kind
    WHEN 'mem_stage1' THEN 0
    WHEN 'mem_consolidate' THEN 1
    WHEN 'mem_summary' THEN 2
    ELSE 3
  END,
  updated_at ASC
LIMIT 1`, args...).Scan(&j.Kind, &j.Scope, &j.JobKey, &j.Status, &j.LastError, &j.RetryRemaining)
	if err == sql.ErrNoRows {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, err
	}
	_, err = i.db.Exec(`UPDATE jobs SET status='running', lease_until=?, updated_at=? WHERE kind=? AND scope=? AND job_key=?`,
		now+leaseSecs, now, j.Kind, j.Scope, j.JobKey)
	return j, err == nil, err
}

// Finish marks a claimed job done (err == nil) or retries/errors it.
func (i *Index) Finish(j Job, jobErr error) error {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	now := time.Now().Unix()
	if jobErr == nil {
		_, err := i.db.Exec(`UPDATE jobs SET status='done', last_error=NULL, updated_at=? WHERE kind=? AND scope=? AND job_key=?`,
			now, j.Kind, j.Scope, j.JobKey)
		return err
	}
	status := "pending"
	retry := j.RetryRemaining - 1
	if retry < 0 {
		status, retry = "error", 0
	}
	_, err := i.db.Exec(`UPDATE jobs SET status=?, retry_remaining=?, last_error=?, lease_until=NULL, updated_at=? WHERE kind=? AND scope=? AND job_key=?`,
		status, retry, truncErr(jobErr), now, j.Kind, j.Scope, j.JobKey)
	return err
}

func truncErr(err error) string {
	s := err.Error()
	if len(s) > 300 {
		return s[:300]
	}
	return s
}

// --- git versioning of the memory dir ---------------------------------------

// CommitMemory commits the whole ~/.eigen/memory tree to a local git repo
// (init on first use), so every consolidation/summary is revertable history.
// Best-effort: git missing or failing is a no-op (the .bak snapshots remain the
// hard safety net). Never pushed anywhere.
func CommitMemory(message string) {
	base, err := baseDir()
	if err != nil {
		return
	}
	if _, err := os.Stat(filepath.Join(base, ".git")); err != nil {
		_ = runGit(base, "init", "-q")
		_ = runGit(base, "config", "user.email", "eigen@localhost")
		_ = runGit(base, "config", "user.name", "eigen")
		// Don't version the sqlite index or its WAL — bookkeeping, not memory.
		_ = os.WriteFile(filepath.Join(base, ".gitignore"), []byte("index.sqlite*\n*.tmp\n"), 0o644)
	}
	_ = runGit(base, "add", "-A")
	// commit only if there's something staged (ignore "nothing to commit")
	_ = runGit(base, "commit", "-q", "-m", message)
}

// runGit runs a git command in dir, returning any error (best-effort caller).
func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}
