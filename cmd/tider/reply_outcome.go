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

var landedStates = []string{
	"landed",
	"ignored",
	"too_long",
	"too_generic",
	"too_operational",
	"too_soft",
}

var kovaSignals = []string{
	"helped",
	"hurt",
	"neutral",
}

var replyOutcomeCmd = &cobra.Command{
	Use:   "outcome <session-id>",
	Short: "Record what happened 2-3 days after a posted reply",
	Long: `Record the engagement outcome of a reply you posted earlier with
` + "`tider reply post`" + `. Run this 2-3 days after posting, when
upvotes / replies / engagement signals have settled.

Requires post.json to already exist in the session — outcomes are
relative to a recorded posting, not a draft.`,
	Args: cobra.ExactArgs(1),
	RunE: runReplyOutcome,
}

func init() {
	replyCmd.AddCommand(replyOutcomeCmd)
}

func runReplyOutcome(cmd *cobra.Command, args []string) error {
	sid := args[0]

	root, err := reply.SessionsRoot()
	if err != nil {
		return err
	}
	sess, err := reply.ResolveSession(root, sid)
	if err != nil {
		return err
	}

	// Outcome only makes sense relative to a posted reply. Reject the
	// outcome-before-post path explicitly so the user knows what to do
	// next instead of getting a confusing missing-file error.
	if !sess.HasFile("post.json") {
		return fmt.Errorf("no post.json in session %s — record the posted reply first with `tider reply post %s`", filepath.Base(sess.Path()), filepath.Base(sess.Path()))
	}

	var post types.ReplyPost
	if err := sess.LoadJSON("post.json", &post); err != nil {
		return fmt.Errorf("read post.json: %w", err)
	}

	fmt.Fprintf(os.Stderr, "session: %s\n", filepath.Base(sess.Path()))
	if post.ThreadURL != "" {
		fmt.Fprintf(os.Stderr, "thread:  %s\n", post.ThreadURL)
	}
	if !post.PostedAt.IsZero() {
		fmt.Fprintf(os.Stderr, "posted:  %s (%s ago)\n",
			post.PostedAt.Local().Format("2006-01-02 15:04"),
			humanAge(time.Since(post.PostedAt)))
	}

	fmt.Fprintln(os.Stderr, strings.Repeat("─", 60))
	fmt.Fprintln(os.Stderr, "posted reply:")
	fmt.Fprintln(os.Stderr, post.FinalText)
	fmt.Fprintln(os.Stderr, strings.Repeat("─", 60))

	if sess.HasFile("outcome.json") {
		in := bufio.NewReader(os.Stdin)
		ok, err := confirmYesNo(in, os.Stderr, "outcome.json already exists for this session. Overwrite?")
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("aborted by user")
		}
	}

	in := bufio.NewReader(os.Stdin)

	upvotes, err := promptInt(in, os.Stderr, "upvotes / score: ")
	if err != nil {
		return err
	}
	opReplied, err := promptYesNo(in, os.Stderr, "did the OP reply?")
	if err != nil {
		return err
	}
	otherEngagement, err := promptYesNo(in, os.Stderr, "any other comment engagement (sub-thread, replies-to-your-reply, etc.)?")
	if err != nil {
		return err
	}
	landed, err := promptChoice(in, os.Stderr, "landed_state", landedStates)
	if err != nil {
		return err
	}
	kova, err := promptChoice(in, os.Stderr, "kova_signal (did the kova angle help, hurt, or stay neutral?)", kovaSignals)
	if err != nil {
		return err
	}
	note := promptLine(in, os.Stderr, "optional note (Enter to skip): ")

	outcome := &types.ReplyOutcome{
		SessionID:              filepath.Base(sess.Path()),
		ThreadURL:              post.ThreadURL,
		PostedAt:               post.PostedAt,
		CheckedAt:              time.Now().UTC(),
		Upvotes:                upvotes,
		OPReplied:              opReplied,
		OtherCommentEngagement: otherEngagement,
		LandedState:            landed,
		KovaSignal:             kova,
		Note:                   strings.TrimSpace(note),
	}
	if err := sess.SaveOutcome(outcome); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "\nwrote %s\n", filepath.Join(sess.Path(), "outcome.json"))
	return nil
}

// promptInt prompts for an integer. Empty input defaults to 0 (a posted
// reply with no upvotes is a real signal, not a missing-data signal).
func promptInt(in *bufio.Reader, out io.Writer, prompt string) (int, error) {
	for {
		fmt.Fprint(out, prompt)
		line, err := in.ReadString('\n')
		if err != nil && err != io.EOF {
			return 0, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return 0, nil
		}
		n, err := strconv.Atoi(line)
		if err != nil {
			fmt.Fprintln(out, "  not a number, try again")
			continue
		}
		return n, nil
	}
}

// promptYesNo blocks until the user answers y/n. Empty input is rejected
// — outcome capture is structured-by-design, so we want a real signal,
// not a default-on-blank.
//
// On stdin EOF (closed pipe / non-interactive invocation) we abort
// rather than re-prompt: ReadString returns "" + io.EOF immediately
// and forever once the underlying file is exhausted, so a naive retry
// loop spins indefinitely.
func promptYesNo(in *bufio.Reader, out io.Writer, prompt string) (bool, error) {
	for {
		fmt.Fprintf(out, "%s [y/n]: ", prompt)
		line, err := in.ReadString('\n')
		if err != nil && err != io.EOF {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		if err == io.EOF {
			return false, errors.New("stdin closed before yes/no answer")
		}
		fmt.Fprintln(out, "  please answer y or n")
	}
}

// promptChoice presents a numbered list of options and parses a number
// or exact-name reply. Loops until the user picks something valid —
// outcome capture is meant to be structured.
//
// On stdin EOF we abort rather than re-prompt; see promptYesNo.
func promptChoice(in *bufio.Reader, out io.Writer, label string, options []string) (string, error) {
	fmt.Fprintf(out, "\n%s:\n", label)
	for i, o := range options {
		fmt.Fprintf(out, "  %d) %s\n", i+1, o)
	}
	known := make(map[string]bool, len(options))
	for _, o := range options {
		known[o] = true
	}
	for {
		fmt.Fprint(out, "select (number or name): ")
		line, err := in.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		entry := strings.ToLower(strings.TrimSpace(line))

		if n, atoiErr := strconv.Atoi(entry); atoiErr == nil {
			if n >= 1 && n <= len(options) {
				return options[n-1], nil
			}
			fmt.Fprintf(out, "  out of range (1-%d)\n", len(options))
			continue
		}
		if known[entry] {
			return entry, nil
		}

		if err == io.EOF {
			return "", fmt.Errorf("stdin closed before %s selection", label)
		}
		if entry == "" {
			fmt.Fprintln(out, "  selection required")
		} else {
			fmt.Fprintln(out, "  not a valid option")
		}
	}
}
