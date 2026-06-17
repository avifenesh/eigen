package memory

import (
	"database/sql"
	"fmt"
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
// The index is advisory but durable: stage1_outputs stores the per-thread
// raw_memory + rollout_summary Codex-style, while the markdown workspace is the
// human-readable Phase 2 surface.
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
	  raw_path     TEXT NOT NULL,   -- legacy path to a materialized rollout summary
  outcome      TEXT,            -- success | partial | failed
  watermark    INTEGER,         -- source transcript mtime/size signature when summarized
  generated_at INTEGER NOT NULL,
  usage_count  INTEGER NOT NULL DEFAULT 0,
  last_used    INTEGER,
	  in_summary   INTEGER NOT NULL DEFAULT 1, -- legacy: whether it currently feeds summary injection
  PRIMARY KEY (scope, session_id)
);
	CREATE TABLE IF NOT EXISTS jobs (
	  kind          TEXT NOT NULL,  -- mem_stage1 | mem_consolidate | mem_summary | mem_forget
	  scope         TEXT NOT NULL,
	  job_key       TEXT NOT NULL,  -- dedup key (e.g. session id for stage1)
	  status        TEXT NOT NULL,  -- pending | running | done | error
	  worker_id     TEXT,
	  ownership_token TEXT,
	  started_at    INTEGER,
	  finished_at   INTEGER,
	  lease_until   INTEGER,        -- a worker holds this job until this unix time
	  retry_at      INTEGER,
	  retry_remaining INTEGER NOT NULL DEFAULT 2,
	  last_error    TEXT,
	  input_watermark INTEGER,
	  last_success_watermark INTEGER,
	  updated_at    INTEGER NOT NULL,
	  PRIMARY KEY (kind, scope, job_key)
	);
	CREATE TABLE IF NOT EXISTS stage1_outputs (
	  scope        TEXT NOT NULL,
	  thread_id    TEXT NOT NULL,
	  source_updated_at INTEGER NOT NULL,
	  raw_memory   TEXT NOT NULL,
	  rollout_summary TEXT NOT NULL,
	  rollout_slug TEXT NOT NULL,
	  rollout_path TEXT,
	  outcome      TEXT,
	  generated_at INTEGER NOT NULL,
	  usage_count  INTEGER NOT NULL DEFAULT 0,
	  last_usage   INTEGER,
	  selected_for_phase2 INTEGER NOT NULL DEFAULT 0,
	  selected_for_phase2_source_updated_at INTEGER,
	  PRIMARY KEY (scope, thread_id)
	);
	CREATE INDEX IF NOT EXISTS idx_stage1_outputs_scope_source ON stage1_outputs(scope, source_updated_at DESC, thread_id DESC);
	`)
	if err != nil {
		return err
	}
	for _, col := range []struct {
		name string
		def  string
	}{
		{"worker_id", "TEXT"},
		{"ownership_token", "TEXT"},
		{"started_at", "INTEGER"},
		{"finished_at", "INTEGER"},
		{"retry_at", "INTEGER"},
		{"input_watermark", "INTEGER"},
		{"last_success_watermark", "INTEGER"},
	} {
		if err := i.ensureColumn("jobs", col.name, col.def); err != nil {
			return err
		}
	}
	if _, err := i.db.Exec(`CREATE INDEX IF NOT EXISTS idx_jobs_kind_status_retry_lease ON jobs(kind, status, retry_at, lease_until)`); err != nil {
		return err
	}
	return nil
}

func (i *Index) ensureColumn(table, name, def string) error {
	rows, err := i.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var col, typ string
		var notNull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &col, &typ, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if col == name {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = i.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + name + ` ` + def)
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

// Stage1Output is Eigen's scoped form of Codex's stage1_outputs row. It keeps
// the model-produced raw_memory and rollout_summary in SQLite first, then the
// markdown workspace can be materialized from it for humans and Phase 2.
type Stage1Output struct {
	Scope, ThreadID, RawMemory, RolloutSummary, RolloutSlug, RolloutPath, Outcome string
	SourceUpdatedAt, GeneratedAt, UsageCount, LastUsage                           int64
	SelectedForPhase2                                                             bool
	SelectedForPhase2SourceUpdatedAt                                              int64
}

// RecordStage1Output upserts a per-session Stage1 row. Older source watermarks
// cannot overwrite newer rows, matching Codex's "source_updated_at wins" rule.
func (i *Index) RecordStage1Output(r Stage1Output) error {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	_, err := i.db.Exec(`
INSERT INTO stage1_outputs (
  scope, thread_id, source_updated_at, raw_memory, rollout_summary, rollout_slug,
  rollout_path, outcome, generated_at, usage_count, last_usage, selected_for_phase2,
  selected_for_phase2_source_updated_at
) VALUES (?,?,?,?,?,?,?,?,?,
  COALESCE((SELECT usage_count FROM stage1_outputs WHERE scope=? AND thread_id=?),0),
  COALESCE((SELECT last_usage FROM stage1_outputs WHERE scope=? AND thread_id=?),NULL),
  0, NULL
)
ON CONFLICT(scope, thread_id) DO UPDATE SET
  source_updated_at=excluded.source_updated_at,
  raw_memory=excluded.raw_memory,
  rollout_summary=excluded.rollout_summary,
  rollout_slug=excluded.rollout_slug,
  rollout_path=excluded.rollout_path,
  outcome=excluded.outcome,
  generated_at=excluded.generated_at,
  selected_for_phase2=0,
  selected_for_phase2_source_updated_at=NULL
WHERE excluded.source_updated_at >= stage1_outputs.source_updated_at`,
		r.Scope, r.ThreadID, r.SourceUpdatedAt, r.RawMemory, r.RolloutSummary, r.RolloutSlug,
		r.RolloutPath, r.Outcome, r.GeneratedAt,
		r.Scope, r.ThreadID, r.Scope, r.ThreadID)
	return err
}

// UpdateStage1RolloutPath records the materialized rollout summary path after
// the DB-first Stage1 row has been written.
func (i *Index) UpdateStage1RolloutPath(scope, threadID, path string) error {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	_, err := i.db.Exec(`UPDATE stage1_outputs SET rollout_path=? WHERE scope=? AND thread_id=?`, path, scope, threadID)
	return err
}

// Stage1Summarized reports whether a thread is summarized at sourceUpdatedAt.
func (i *Index) Stage1Summarized(scope, threadID string, sourceUpdatedAt int64) bool {
	if i == nil {
		return false
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	var wm int64
	err := i.db.QueryRow(`SELECT source_updated_at FROM stage1_outputs WHERE scope=? AND thread_id=?`, scope, threadID).Scan(&wm)
	return err == nil && wm == sourceUpdatedAt && sourceUpdatedAt != 0
}

// Stage1Outputs lists a scope's Stage1 rows, newest first.
func (i *Index) Stage1Outputs(scope string, limit int) ([]Stage1Output, error) {
	if i == nil {
		return nil, nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	q := `SELECT scope,thread_id,source_updated_at,raw_memory,rollout_summary,rollout_slug,COALESCE(rollout_path,''),COALESCE(outcome,''),generated_at,usage_count,COALESCE(last_usage,0),selected_for_phase2,COALESCE(selected_for_phase2_source_updated_at,0)
FROM stage1_outputs WHERE scope=? ORDER BY source_updated_at DESC, thread_id DESC`
	args := []any{scope}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := i.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Stage1Output
	for rows.Next() {
		var r Stage1Output
		var selected int
		if err := rows.Scan(&r.Scope, &r.ThreadID, &r.SourceUpdatedAt, &r.RawMemory, &r.RolloutSummary, &r.RolloutSlug, &r.RolloutPath, &r.Outcome, &r.GeneratedAt, &r.UsageCount, &r.LastUsage, &selected, &r.SelectedForPhase2SourceUpdatedAt); err != nil {
			return nil, err
		}
		r.SelectedForPhase2 = selected != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// Phase2Inputs returns selected Stage1 rows for consolidation. It favors rows
// that have not yet been selected at their current source watermark, then recent
// and frequently used rows as context.
func (i *Index) Phase2Inputs(scope string, limit int) ([]Stage1Output, error) {
	if i == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 64
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	rows, err := i.db.Query(`SELECT scope,thread_id,source_updated_at,raw_memory,rollout_summary,rollout_slug,COALESCE(rollout_path,''),COALESCE(outcome,''),generated_at,usage_count,COALESCE(last_usage,0),selected_for_phase2,COALESCE(selected_for_phase2_source_updated_at,0)
FROM stage1_outputs
WHERE scope=? AND (TRIM(raw_memory) <> '' OR TRIM(rollout_summary) <> '')
ORDER BY
  CASE WHEN selected_for_phase2_source_updated_at IS NULL OR selected_for_phase2_source_updated_at < source_updated_at THEN 0 ELSE 1 END,
  usage_count DESC,
  COALESCE(last_usage, source_updated_at) DESC,
  source_updated_at DESC
LIMIT ?`, scope, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Stage1Output
	for rows.Next() {
		var r Stage1Output
		var selected int
		if err := rows.Scan(&r.Scope, &r.ThreadID, &r.SourceUpdatedAt, &r.RawMemory, &r.RolloutSummary, &r.RolloutSlug, &r.RolloutPath, &r.Outcome, &r.GeneratedAt, &r.UsageCount, &r.LastUsage, &selected, &r.SelectedForPhase2SourceUpdatedAt); err != nil {
			return nil, err
		}
		r.SelectedForPhase2 = selected != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkSelectedForPhase2 records which source watermarks fed Phase 2.
func (i *Index) MarkSelectedForPhase2(rows []Stage1Output) {
	if i == nil || len(rows) == 0 {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, r := range rows {
		_, _ = i.db.Exec(`UPDATE stage1_outputs SET selected_for_phase2=1, selected_for_phase2_source_updated_at=? WHERE scope=? AND thread_id=? AND source_updated_at=?`, r.SourceUpdatedAt, r.Scope, r.ThreadID, r.SourceUpdatedAt)
	}
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
	if i.Stage1Summarized(scope, sessionID, watermark) {
		return true
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
		_, _ = i.db.Exec(`UPDATE stage1_outputs SET usage_count=usage_count+1, last_usage=? WHERE scope=? AND thread_id=?`, now, scope, id)
	}
}

// Summaries lists a scope's summary rows, newest first.
func (i *Index) Summaries(scope string) ([]SummaryRow, error) {
	if i == nil {
		return nil, nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	rows, err := i.db.Query(`SELECT scope,thread_id,rollout_slug,COALESCE(rollout_path,''),COALESCE(outcome,''),source_updated_at,generated_at,usage_count,COALESCE(last_usage,0),selected_for_phase2 FROM stage1_outputs WHERE scope=? ORDER BY generated_at DESC`, scope)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > 0 {
		return out, nil
	}
	rows, err = i.db.Query(`SELECT scope,session_id,slug,raw_path,COALESCE(outcome,''),COALESCE(watermark,0),generated_at,usage_count,COALESCE(last_used,0),in_summary FROM summaries WHERE scope=? ORDER BY generated_at DESC`, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
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
	return i.EnqueueWatermark(kind, scope, jobKey, 0)
}

// EnqueueWatermark adds a job with an optional input watermark, Codex-style.
func (i *Index) EnqueueWatermark(kind, scope, jobKey string, inputWatermark int64) error {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	now := time.Now().Unix()
	_, err := i.db.Exec(`
	INSERT INTO jobs (kind, scope, job_key, status, retry_remaining, retry_at, input_watermark, updated_at)
	VALUES (?,?,?, 'pending', 2, ?, ?, ?)
	ON CONFLICT(kind, scope, job_key) DO UPDATE SET
	  status=CASE WHEN jobs.status IN ('done','error') OR COALESCE(excluded.input_watermark,0) > COALESCE(jobs.last_success_watermark,-1) THEN 'pending' ELSE jobs.status END,
	  retry_at=excluded.retry_at,
	  input_watermark=CASE WHEN excluded.input_watermark > COALESCE(jobs.input_watermark,0) THEN excluded.input_watermark ELSE jobs.input_watermark END,
	  finished_at=NULL,
	  updated_at=excluded.updated_at`,
		kind, scope, jobKey, now, inputWatermark, now)
	return err
}

// Job is a queued memory job.
type Job struct {
	Kind, Scope, JobKey, Status, LastError, WorkerID, OwnershipToken string
	RetryRemaining                                                   int
	InputWatermark, LastSuccessWatermark                             int64
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
	where := "WHERE (status='pending' AND COALESCE(retry_at,0) <= ?) OR (status='running' AND COALESCE(lease_until,0) < ?)"
	args := []any{now, now}
	if scope != "" {
		where = "WHERE scope=? AND ((status='pending' AND COALESCE(retry_at,0) <= ?) OR (status='running' AND COALESCE(lease_until,0) < ?))"
		args = []any{scope, now, now}
	}
	err := i.db.QueryRow(`
	SELECT kind,scope,job_key,status,COALESCE(last_error,''),retry_remaining,COALESCE(input_watermark,0),COALESCE(last_success_watermark,0) FROM jobs
	`+where+`
ORDER BY
  CASE kind
    WHEN 'mem_stage1' THEN 0
    WHEN 'mem_consolidate' THEN 1
    WHEN 'mem_summary' THEN 2
    ELSE 3
  END,
  updated_at ASC
	LIMIT 1`, args...).Scan(&j.Kind, &j.Scope, &j.JobKey, &j.Status, &j.LastError, &j.RetryRemaining, &j.InputWatermark, &j.LastSuccessWatermark)
	if err == sql.ErrNoRows {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, err
	}
	j.WorkerID = workerID()
	j.OwnershipToken = workerID() + "-" + j.Kind + "-" + j.Scope + "-" + j.JobKey
	_, err = i.db.Exec(`UPDATE jobs SET status='running', worker_id=?, ownership_token=?, started_at=?, finished_at=NULL, lease_until=?, updated_at=? WHERE kind=? AND scope=? AND job_key=?`,
		j.WorkerID, j.OwnershipToken, now, now+leaseSecs, now, j.Kind, j.Scope, j.JobKey)
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
		_, err := i.db.Exec(`UPDATE jobs SET status='done', finished_at=?, lease_until=NULL, last_error=NULL, last_success_watermark=COALESCE(input_watermark,last_success_watermark), updated_at=? WHERE kind=? AND scope=? AND job_key=?`,
			now, now, j.Kind, j.Scope, j.JobKey)
		return err
	}
	status := "pending"
	retry := j.RetryRemaining - 1
	if retry < 0 {
		status, retry = "error", 0
	}
	retryAt := now
	_, err := i.db.Exec(`UPDATE jobs SET status=?, retry_remaining=?, last_error=?, retry_at=?, lease_until=NULL, finished_at=?, updated_at=? WHERE kind=? AND scope=? AND job_key=?`,
		status, retry, truncErr(jobErr), retryAt, now, now, j.Kind, j.Scope, j.JobKey)
	return err
}

func workerID() string {
	return fmt.Sprintf("eigen-%s-%d", time.Now().Format("20060102T150405"), os.Getpid())
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
