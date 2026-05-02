package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/viggy28/tider/internal/types"
	"github.com/viggy28/tider/reply"
)

const outcomeDueAfter = 48 * time.Hour

var replyRecentLimit int

var replyRecentCmd = &cobra.Command{
	Use:   "recent",
	Short: "List recent reply sessions with their lifecycle status",
	Long: `Show the most recent reply sessions, newest first. Useful for
finding the right session id to feed to ` + "`tider reply post`" + ` or
` + "`tider reply outcome`" + `.

Status values:
  drafted          drafts.json exists, no post.json yet
  posted           post.json exists, no outcome.json yet
  outcome-recorded outcome.json exists
  failed           session dir exists but drafts.json was never written

Posted sessions older than 48h are also flagged "outcome-due" — that's
when the post is mature enough for engagement signals to settle.`,
	RunE: runReplyRecent,
}

func init() {
	replyRecentCmd.Flags().IntVarP(&replyRecentLimit, "limit", "n", 10, "max sessions to display (newest first)")
	replyCmd.AddCommand(replyRecentCmd)
}

func runReplyRecent(cmd *cobra.Command, args []string) error {
	root, err := reply.SessionsRoot()
	if err != nil {
		return err
	}
	sessions, err := reply.ListSessions(root, replyRecentLimit)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Fprintln(os.Stderr, "no reply sessions found at", root)
		return nil
	}

	now := time.Now()
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SESSION\tSUB\tMODE\tAGE\tSTATUS\tTITLE")
	for _, s := range sessions {
		row := buildRecentRow(s, now)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			row.id, row.sub, row.mode, row.age, row.status, row.title)
	}
	return w.Flush()
}

type recentRow struct {
	id     string
	sub    string
	mode   string
	age    string
	status string
	title  string
}

// buildRecentRow assembles one display row from a session directory by
// reading whatever metadata is on disk. Each load is best-effort because
// older / failed sessions may be missing thread.json or mode.json — the
// row should still render with hyphens for the missing columns rather
// than crashing the whole listing.
func buildRecentRow(s *reply.Session, now time.Time) recentRow {
	id := filepath.Base(s.Path())
	row := recentRow{
		id:     id,
		sub:    "-",
		mode:   "-",
		title:  "-",
		status: string(s.Status()),
	}

	var thread types.Thread
	if err := s.LoadJSON("thread.json", &thread); err == nil {
		if thread.Subreddit != "" {
			row.sub = "r/" + thread.Subreddit
		}
		if thread.Title != "" {
			row.title = truncate(thread.Title, 60)
		}
	}

	var mode types.ReplyModeResult
	if err := s.LoadJSON("mode.json", &mode); err == nil && mode.Mode != "" {
		row.mode = string(mode.Mode)
	}

	// Age: prefer Generated time on the bundle (drafter completion is
	// the canonical "session became useful" moment), fall back to dir
	// mtime which NewSession sets at create time.
	created := dirMTime(s.Path(), now)
	var bundle types.ReplyBundle
	if err := s.LoadJSON("drafts.json", &bundle); err == nil && !bundle.Generated.IsZero() {
		created = bundle.Generated
	}
	row.age = humanAge(now.Sub(created))

	// Outcome-due annotation: posted but past the 48h soak window. Only
	// applies to "posted" — drafted/failed haven't reached that stage,
	// outcome-recorded is past it.
	if row.status == string(reply.SessionStatusPosted) {
		var post types.ReplyPost
		postedAt := created
		if err := s.LoadJSON("post.json", &post); err == nil && !post.PostedAt.IsZero() {
			postedAt = post.PostedAt
		}
		if now.Sub(postedAt) >= outcomeDueAfter {
			row.status = row.status + " (outcome-due)"
		}
	}

	return row
}

func dirMTime(path string, fallback time.Time) time.Time {
	fi, err := os.Stat(path)
	if err != nil {
		return fallback
	}
	return fi.ModTime()
}

// humanAge renders a duration as a single-unit, coarse-grained label
// like "3h" or "2d". The recent list is for at-a-glance triage, not
// exact times — precision past the leading unit is noise.
func humanAge(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\t", " ")
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
