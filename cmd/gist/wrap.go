// Wrap runs any command with transparent I/O capture and prompt alignment.
//
// Usage:
//
//	gist wrap [--dir <path>] [--quiet] -- <command> [args...]
//
// Gist spawns <command> with stdin/stdout/stderr piped through the current
// process. Every chunk of I/O is logged to a JSONL file under
// ~/.config/gist/captures/ by default. When a chunk on stdin looks like a
// prompt (JSON payload, code fence, markdown heading, or long text), the
// pkg/aligner is invoked and the optimized payload is attached to the
// recorded event.
//
// Wrap is fully passive: it never modifies the bytes flowing between the
// user and the wrapped tool. The capture is "always on" so the user can
// audit, replay, or post-process sessions after the fact.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/elbader17/gist/pkg/capture"
)

// WrapOptions configures the wrap subcommand.
type WrapOptions struct {
	Dir    string
	Quiet  bool
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// RunWrap executes the wrap subcommand. It returns the exit code of the
// wrapped process (or 1 on setup error).
func RunWrap(command string, args []string, opts WrapOptions) int {
	if command == "" {
		fmt.Fprintln(opts.Stderr, "gist wrap: missing command after `--`")
		return 1
	}

	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}

	sess, err := capture.Open(capture.Options{
		Command: command,
		Args:    args,
		Dir:     opts.Dir,
	})
	if err != nil {
		fmt.Fprintf(opts.Stderr, "gist wrap: open capture: %v\n", err)
		return 1
	}

	if !opts.Quiet {
		fmt.Fprintf(opts.Stderr, "gist: capturing session to %s\n", sess.Path())
	}

	cmd := exec.Command(command, args...)
	stdinR, stdinW := io.Pipe()
	cmd.Stdin = stdinR

	stdoutR, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(opts.Stderr, "gist wrap: stdout pipe: %v\n", err)
		_ = sess.Close(1)
		return 1
	}
	stderrR, err := cmd.StderrPipe()
	if err != nil {
		fmt.Fprintf(opts.Stderr, "gist wrap: stderr pipe: %v\n", err)
		_ = sess.Close(1)
		return 1
	}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(opts.Stderr, "gist wrap: start %q: %v\n", command, err)
		_ = sess.Close(1)
		return 1
	}

	stdinDone := make(chan struct{})
	go func() {
		defer close(stdinDone)
		defer stdinW.Close()
		scanner := bufio.NewScanner(opts.Stdin)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			sess.Record(capture.DirStdin, line)
			if _, err := io.WriteString(stdinW, line); err != nil {
				return
			}
			if _, err := io.WriteString(stdinW, "\n"); err != nil {
				return
			}
		}
	}()

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(opts.Stdout, stdoutR)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(opts.Stderr, stderrR)
		done <- struct{}{}
	}()
	<-done
	<-done
	<-stdinDone

	waitErr := cmd.Wait()
	exitCode := 0
	if waitErr != nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			exitCode = 1
		}
	}

	if err := sess.Close(exitCode); err != nil && !opts.Quiet {
		fmt.Fprintf(opts.Stderr, "gist wrap: close capture: %v\n", err)
	}

	if !opts.Quiet && sess.Prompts() > 0 {
		fmt.Fprintf(opts.Stderr,
			"gist: detected %d prompt(s) — inspect %s\n",
			sess.Prompts(), sess.Path(),
		)
	}

	return exitCode
}