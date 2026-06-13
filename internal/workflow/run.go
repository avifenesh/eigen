package workflow

import (
	"context"
	"fmt"
	"strings"
)

// StepRunner executes one step's prompt and returns the final answer. The model
// is the step's explicit model ("" = inherit the session's). Implemented by the
// caller (main), which owns the agent/session — the workflow package stays free
// of agent/llm deps.
type StepRunner func(ctx context.Context, prompt, model string) (string, error)

// Judge verifies a step's output against a condition (the goal judge). Returns
// (ok, reason). Implemented by the caller.
type Judge func(ctx context.Context, condition, output string) (bool, string, error)

// Reporter receives progress events during a run (step start/result/check), so
// the headless runner can stream to stderr and the TUI can show progress.
type Reporter func(ev Event)

// Event is one progress signal.
type Event struct {
	Kind    string // "step" | "result" | "check" | "retry" | "error" | "done"
	StepID  string
	Attempt int
	Text    string // result excerpt / check reason / error
	OK      bool   // for "check"/"done"
}

// RunOpts configures a run.
type RunOpts struct {
	Vars   map[string]string
	Run    StepRunner
	Judge  Judge // may be nil → steps with a check fail closed with a clear error
	Report Reporter
}

// Result summarizes a finished run.
type Result struct {
	Completed []string // step ids that ran ok
	FailedAt  string   // step id that stopped the run ("" = all ok)
	Outputs   map[string]string
}

// Run executes the workflow's steps in order on ONE carried session (the
// StepRunner sends each prompt to the same session, so step N sees prior work).
// A step's optional check is judged; on failure the step's on_failure policy
// applies (stop aborts with a non-nil error, continue records and proceeds,
// retry re-runs up to Retries times). Returns the Result and a non-nil error
// only when a stop-on-failure step failed (so `eigen run` can exit-code it).
func (wf *Workflow) Run(ctx context.Context, opts RunOpts) (*Result, error) {
	res := &Result{Outputs: map[string]string{}}
	report := opts.Report
	if report == nil {
		report = func(Event) {}
	}
	for _, step := range wf.Steps {
		prompt, missing := Interpolate(step.Prompt, opts.Vars)
		if len(missing) > 0 {
			report(Event{Kind: "error", StepID: step.ID, Text: "unset --var: " + strings.Join(missing, ", ")})
		}
		report(Event{Kind: "step", StepID: step.ID})

		attempts := 1
		if step.OnFailure == FailRetry {
			attempts = step.Retries + 1
		}
		var out string
		var ok bool
		var lastReason string
		for attempt := 1; attempt <= attempts; attempt++ {
			if ctx.Err() != nil {
				return res, ctx.Err()
			}
			if attempt > 1 {
				report(Event{Kind: "retry", StepID: step.ID, Attempt: attempt})
			}
			var err error
			out, err = opts.Run(ctx, prompt, step.Model)
			if err != nil {
				report(Event{Kind: "error", StepID: step.ID, Text: err.Error()})
				return res, fmt.Errorf("step %q: %w", step.ID, err)
			}
			res.Outputs[step.ID] = out
			report(Event{Kind: "result", StepID: step.ID, Text: excerpt(out)})

			if step.Check == "" {
				ok = true
				break
			}
			// Judged check.
			if opts.Judge == nil {
				return res, fmt.Errorf("step %q has a check but no judge is configured", step.ID)
			}
			passed, reason, jerr := opts.Judge(ctx, step.Check, out)
			if jerr != nil {
				return res, fmt.Errorf("step %q: check failed to run: %w", step.ID, jerr)
			}
			lastReason = reason
			report(Event{Kind: "check", StepID: step.ID, OK: passed, Text: reason})
			if passed {
				ok = true
				break
			}
		}
		if ok {
			res.Completed = append(res.Completed, step.ID)
			continue
		}
		// Check failed after all attempts → apply on_failure.
		switch step.OnFailure {
		case FailContinue:
			report(Event{Kind: "result", StepID: step.ID, Text: "check failed, continuing: " + lastReason})
			res.Completed = append(res.Completed, step.ID)
		default: // stop (and retry-exhausted)
			res.FailedAt = step.ID
			report(Event{Kind: "done", OK: false, StepID: step.ID, Text: "stopped: check failed: " + lastReason})
			return res, fmt.Errorf("workflow stopped at step %q: check failed: %s", step.ID, lastReason)
		}
	}
	report(Event{Kind: "done", OK: true})
	return res, nil
}

func excerpt(s string) string {
	s = strings.TrimSpace(s)
	const max = 200
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
