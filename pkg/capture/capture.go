// Package capture implements transparent I/O capture for the `gist wrap`
// command.
//
// When you run `gist wrap -- <cmd>`, Gist spawns <cmd> with its stdio
// connected through a Capture Session. Every byte that flows in either
// direction is:
//
//  1. Forwarded to the real peer (so the wrapped tool behaves identically).
//  2. Recorded as a CaptureEvent in a JSONL file under
//     ~/.config/gist/captures/<timestamp>-<pid>.jsonl.
//  3. Scanned by IsLikelyPrompt. When a prompt is detected, the
//     pkg/aligner is invoked and the optimized payload is attached to the
//     event for later inspection.
//
// The wrapper is fully passive: it never rewrites what the user or the
// wrapped tool actually see. Capture is "always on" by default so the user
// can audit, replay, or post-process sessions after the fact.
package capture

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/elbader17/gist/pkg/aligner"
	"github.com/elbader17/gist/pkg/config"
)

// Direction identifies which stream an event came from.
type Direction string

// Stream directions.
const (
	DirStdin  Direction = "stdin"
	DirStdout Direction = "stdout"
	DirStderr Direction = "stderr"
)

// CaptureEvent is a single recorded chunk of I/O.
type CaptureEvent struct {
	Timestamp time.Time           `json:"timestamp"`
	Direction Direction           `json:"direction"`
	Command   string              `json:"command"`
	Data      string              `json:"data,omitempty"`
	IsPrompt  bool                `json:"is_prompt"`
	Aligned   *aligner.AlignedPayload `json:"aligned,omitempty"`
	PromptKind string             `json:"prompt_kind,omitempty"`
}

// SessionHeader is written as the first line of a capture file.
type SessionHeader struct {
	Kind      string    `json:"kind"`
	Version   string    `json:"version"`
	StartedAt time.Time `json:"started_at"`
	Command   string    `json:"command"`
	Args      []string  `json:"args"`
	PID       int       `json:"pid"`
}

// SessionSummary is written as the last line of a capture file.
type SessionSummary struct {
	Kind        string    `json:"kind"`
	EndedAt     time.Time `json:"ended_at"`
	ExitCode    int       `json:"exit_code"`
	BytesStdin  int64     `json:"bytes_stdin"`
	BytesStdout int64     `json:"bytes_stdout"`
	BytesStderr int64     `json:"bytes_stderr"`
	Prompts     int       `json:"prompts_detected"`
}

// CapturesDir returns the directory where capture files are stored.
// Defaults to ~/.config/gist/captures/; override with GIST_CAPTURES_DIR.
func CapturesDir() (string, error) {
	if d := os.Getenv("GIST_CAPTURES_DIR"); d != "" {
		return d, nil
	}
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "captures"), nil
}

// Session is an open capture session writing JSONL events to disk.
type Session struct {
	mu         sync.Mutex
	file       *os.File
	encoder    *json.Encoder
	Header     SessionHeader
	bytesIn    int64
	bytesOut   int64
	bytesErr   int64
	promptCnt  int
	closed     bool
}

// Options configure a new Session.
type Options struct {
	Command string
	Args    []string
	Dir     string
}

// Open creates (or truncates) a new capture file and writes the session
// header.
func Open(opts Options) (*Session, error) {
	dir := opts.Dir
	if dir == "" {
		var err error
		dir, err = CapturesDir()
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	ts := time.Now().UTC()
	name := fmt.Sprintf("%s-%d.jsonl",
		ts.Format("20060102T150405.000"),
		os.Getpid(),
	)
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	s := &Session{
		file:    f,
		encoder: json.NewEncoder(f),
		Header: SessionHeader{
			Kind:      "gist.capture.header/v1",
			Version:   "0.1.0",
			StartedAt: ts,
			Command:   opts.Command,
			Args:      opts.Args,
			PID:       os.Getpid(),
		},
	}
	if err := s.encoder.Encode(s.Header); err != nil {
		_ = f.Close()
		return nil, err
	}
	return s, nil
}

// Path returns the on-disk path of the capture file.
func (s *Session) Path() string {
	if s.file == nil {
		return ""
	}
	return s.file.Name()
}

// Record appends a raw capture event to the log and, if the data looks like
// a prompt, attaches the aligned payload.
func (s *Session) Record(direction Direction, data string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch direction {
	case DirStdin:
		s.bytesIn += int64(len(data))
	case DirStdout:
		s.bytesOut += int64(len(data))
	case DirStderr:
		s.bytesErr += int64(len(data))
	}

	evt := CaptureEvent{
		Timestamp: time.Now().UTC(),
		Direction: direction,
		Command:   s.Header.Command,
		Data:      data,
	}

	if direction == DirStdin && IsLikelyPrompt(data) {
		evt.IsPrompt = true
		evt.PromptKind = Classify(data)
		sys, static := ExtractLayers(data)
		evt.Aligned = aligner.Align(sys, static, data, "")
		s.promptCnt++
	}

	_ = s.encoder.Encode(evt)
}

// Close writes the summary line and closes the underlying file. Safe to call
// multiple times.
func (s *Session) Close(exitCode int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true

	summary := SessionSummary{
		Kind:        "gist.capture.summary/v1",
		EndedAt:     time.Now().UTC(),
		ExitCode:    exitCode,
		BytesStdin:  s.bytesIn,
		BytesStdout: s.bytesOut,
		BytesStderr: s.bytesErr,
		Prompts:     s.promptCnt,
	}
	if err := s.encoder.Encode(summary); err != nil {
		_ = s.file.Close()
		return err
	}
	return s.file.Close()
}

// Prompts returns the number of prompt-like inputs observed so far.
func (s *Session) Prompts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.promptCnt
}

// TeeReader returns an io.Reader that forwards every byte read from src to
// dst (typically os.Stdout for DirStdout / DirStderr events) and logs each
// newline-delimited chunk to the session.
func (s *Session) TeeReader(src io.Reader, direction Direction, dst io.Writer) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		scanner := bufio.NewScanner(src)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			s.Record(direction, line)
			if dst != nil {
				if _, err := io.WriteString(dst, line); err != nil {
					_ = pw.CloseWithError(err)
					return
				}
				if _, err := io.WriteString(dst, "\n"); err != nil {
					_ = pw.CloseWithError(err)
					return
				}
			}
			if _, err := io.WriteString(pw, line); err != nil {
				return
			}
			if _, err := io.WriteString(pw, "\n"); err != nil {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
	}()
	return pr
}