package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/viggy28/tider/internal/types"
	"github.com/viggy28/tider/reply"
)

// editDeltaTags is the v1 list of "how did the posted text differ from
// what tider drafted" tags presented at `tider reply post` time. Order
// mirrors the issue clarification (#36 comment) so the menu numbering
// stays stable across runs.
var editDeltaTags = []string{
	"keep-mostly-same",
	"shorter",
	"rewrote-thesis",
	"dropped-checklist",
	"added-personal-opener",
	"added-kova",
	"removed-kova",
	"moved-earlier",
	"more-direct",
}

var replyPostCmd = &cobra.Command{
	Use:   "post <session-id>",
	Short: "Record the final text that was actually posted to Reddit",
	Long: `Record the reply you posted manually to Reddit, writing it back
into the originating session under post.json.

Workflow:
  1. tider reply --url=...    (generates drafts)
  2. you edit + post manually on reddit.com
  3. tider reply post <session-id>   (paste final text, capture deltas)

The session id is the directory name printed by the original
` + "`tider reply`" + ` invocation, e.g. 2026-05-02-shopify-1abc23.
Any unique substring (commonly just the postID) is also accepted.

Paste your final reply when prompted; press Ctrl-D to end input.`,
	Args: cobra.ExactArgs(1),
	RunE: runReplyPost,
}

func init() {
	replyCmd.AddCommand(replyPostCmd)
}

func runReplyPost(cmd *cobra.Command, args []string) error {
	sid := args[0]

	root, err := reply.SessionsRoot()
	if err != nil {
		return err
	}
	sess, err := reply.ResolveSession(root, sid)
	if err != nil {
		return err
	}

	// Best-effort thread load — drives the session header and the
	// thread_url field on post.json. A failed-state session may have no
	// thread.json, but we still allow `reply post` so the user can
	// record what they actually wrote.
	var thread types.Thread
	hasThread := sess.LoadJSON("thread.json", &thread) == nil

	fmt.Fprintf(os.Stderr, "session: %s\n", filepath.Base(sess.Path()))
	if hasThread {
		fmt.Fprintf(os.Stderr, "thread:  %s\n", thread.Title)
		fmt.Fprintf(os.Stderr, "sub:     r/%s\n", thread.Subreddit)
	}

	// Use a single buffered reader for every interactive prompt so that
	// subsequent line reads after the multiline paste don't race against
	// each other on stdin.
	in := bufio.NewReader(os.Stdin)

	// Idempotency: existing post.json gets overwritten only with explicit
	// confirmation. We want the loop to be append-friendly without
	// silently destroying a prior capture.
	if sess.HasFile("post.json") {
		ok, err := confirmYesNo(in, os.Stderr, "post.json already exists for this session. Overwrite?")
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("aborted by user")
		}
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Paste the final posted reply. Press Ctrl-D when done.")
	fmt.Fprintln(os.Stderr, strings.Repeat("─", 60))

	finalText, err := readMultiline(in)
	if err != nil {
		return fmt.Errorf("read reply text: %w", err)
	}
	finalText = strings.TrimRight(finalText, "\n")
	if strings.TrimSpace(finalText) == "" {
		return errors.New("no reply text captured")
	}

	fmt.Fprintln(os.Stderr, strings.Repeat("─", 60))
	fmt.Fprintln(os.Stderr, "captured:")
	fmt.Fprintln(os.Stderr, finalText)
	fmt.Fprintln(os.Stderr, strings.Repeat("─", 60))

	// After Ctrl-D the underlying bufio.Reader is at EOF. Subsequent
	// prompts re-read from os.Stdin directly via a fresh reader, which
	// works in a TTY (the EOT only ends the current read) but will
	// silently default for piped input. That's the right tradeoff: the
	// piped case is "I have a script, just save the text" — no tags
	// expected.
	prompts := bufio.NewReader(os.Stdin)
	feedback := promptTags(prompts, os.Stderr, editDeltaTags)
	note := promptLine(prompts, os.Stderr, "optional note (Enter to skip): ")

	threadURL := ""
	if hasThread {
		threadURL = thread.URL
	}

	post := &types.ReplyPost{
		SessionID: filepath.Base(sess.Path()),
		ThreadURL: threadURL,
		FinalText: finalText,
		PostedAt:  time.Now().UTC(),
		Feedback:  feedback,
		Note:      strings.TrimSpace(note),
	}
	if err := sess.SavePost(post); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\nwrote %s\n", filepath.Join(sess.Path(), "post.json"))
	if len(feedback) > 0 {
		fmt.Fprintf(os.Stderr, "tags:  %s\n", strings.Join(feedback, ", "))
	}
	return nil
}

// readMultiline reads from r until EOF (Ctrl-D on a TTY) and returns the
// accumulated text. Empty lines inside the text are preserved verbatim
// because Reddit replies often contain paragraph breaks.
func readMultiline(r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// confirmYesNo prompts for a yes/no answer. Treats blank as "no" — the
// safer default for destructive operations like overwrite.
func confirmYesNo(in *bufio.Reader, out io.Writer, prompt string) (bool, error) {
	fmt.Fprintf(out, "%s [y/N]: ", prompt)
	line, err := in.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// promptTags presents a numbered tag list and parses a comma-separated
// selection (numbers or names). Empty input means "no tags". Unknown
// entries are silently dropped — the loop stays light, the user can
// re-run if they care about precision.
func promptTags(in *bufio.Reader, out io.Writer, tags []string) []string {
	fmt.Fprintln(out, "\nedit-delta tags (how did the posted text diverge from the draft?):")
	for i, t := range tags {
		fmt.Fprintf(out, "  %d) %s\n", i+1, t)
	}
	fmt.Fprintf(out, "select (comma-separated numbers or names, Enter to skip): ")

	line, err := in.ReadString('\n')
	if err != nil && err != io.EOF {
		return nil
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	known := make(map[string]bool, len(tags))
	for _, t := range tags {
		known[t] = true
	}

	var picked []string
	seen := make(map[string]bool)
	for _, raw := range strings.Split(line, ",") {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if n, err := strconv.Atoi(entry); err == nil {
			if n >= 1 && n <= len(tags) {
				tag := tags[n-1]
				if !seen[tag] {
					picked = append(picked, tag)
					seen[tag] = true
				}
			}
			continue
		}
		entry = strings.ToLower(entry)
		if known[entry] && !seen[entry] {
			picked = append(picked, entry)
			seen[entry] = true
		}
	}
	return picked
}

// promptLine prints a prompt and reads one line from in. EOF (piped
// stdin already drained) returns empty silently — the caller decides
// whether emptiness is OK.
func promptLine(in *bufio.Reader, out io.Writer, prompt string) string {
	fmt.Fprint(out, prompt)
	line, err := in.ReadString('\n')
	if err != nil && err != io.EOF {
		return ""
	}
	return strings.TrimRight(line, "\n")
}
