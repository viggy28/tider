// Package progress prints compact, stage-based status to stderr so users
// can tell what a long-running command is doing without overwhelming the
// terminal. One line per stage, plus a final "done in Xs" summary.
//
// Output is line-buffered and append-only — no spinners, no \r rewrites,
// no goroutines. All writes go to the configured writer (os.Stderr in
// production); stdout is reserved for final command output so piping
// stays clean.
package progress

import (
	"fmt"
	"io"
	"time"
)

// Options configures a Reporter at construction time.
//
// The two flags are independent so callers can wire CLI flags directly:
//   --quiet       → Quiet=true       (suppress everything)
//   --no-progress → NoProgress=true  (suppress stages, keep warnings)
type Options struct {
	Quiet      bool
	NoProgress bool
}

// Reporter prints stage-based progress to its writer.
type Reporter struct {
	w            io.Writer
	showProgress bool
	showWarn     bool
	total        int
	cur          int
	cmdStart     time.Time
}

// New returns a Reporter that writes to w.
func New(w io.Writer, opts Options) *Reporter {
	return &Reporter{
		w:            w,
		showProgress: !opts.Quiet && !opts.NoProgress,
		showWarn:     !opts.Quiet,
	}
}

// All methods are nil-safe so callers and tests can pass a nil
// *Reporter without conditional guards; nil acts like a fully-disabled
// reporter that drops every call.

// Start records the total stage count and the command's start time.
// Must be called before Step/Done; safe to call on a disabled reporter.
func (r *Reporter) Start(total int) {
	if r == nil {
		return
	}
	r.total = total
	r.cur = 0
	r.cmdStart = time.Now()
}

// SetTotal updates the total stage count mid-run. Used when the total
// depends on a runtime decision (e.g. tider reply switches from 5 stages
// to 8 once the classifier detects review mode). Lines already printed
// keep their original total; subsequent Step calls use the new value.
func (r *Reporter) SetTotal(total int) {
	if r == nil {
		return
	}
	r.total = total
}

// Step advances the stage counter and prints "[N/M] label".
func (r *Reporter) Step(label string) {
	if r == nil || !r.showProgress {
		return
	}
	r.cur++
	fmt.Fprintf(r.w, "[%d/%d] %s\n", r.cur, r.total, label)
}

// Update prints an indented annotation line under the current step,
// e.g. classifier results: "  → reply".
func (r *Reporter) Update(label string) {
	if r == nil || !r.showProgress {
		return
	}
	fmt.Fprintf(r.w, "  → %s\n", label)
}

// Done prints the final "done in Xs" summary using the elapsed command time.
func (r *Reporter) Done() {
	if r == nil || !r.showProgress {
		return
	}
	if r.cmdStart.IsZero() {
		return
	}
	fmt.Fprintf(r.w, "done in %s\n", FormatDuration(time.Since(r.cmdStart)))
}

// Warn prints a warning regardless of --no-progress; suppressed by --quiet.
func (r *Reporter) Warn(format string, args ...any) {
	if r == nil || !r.showWarn {
		return
	}
	fmt.Fprintf(r.w, "warning: "+format+"\n", args...)
}

// SessionPath prints the session directory once, before any long-running
// work, so users can find the artifacts even if the run aborts later.
func (r *Reporter) SessionPath(path string) {
	if r == nil || !r.showProgress {
		return
	}
	fmt.Fprintf(r.w, "session: %s\n", path)
}

// FormatDuration renders a duration as "Ns" or "NmSSs" (always
// two-digit seconds when minutes are present), matching the style used
// in issue #20: 38s, 1m12s, 1m03s.
func FormatDuration(d time.Duration) string {
	sec := int(d.Round(time.Second).Seconds())
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	m := sec / 60
	s := sec % 60
	return fmt.Sprintf("%dm%02ds", m, s)
}
