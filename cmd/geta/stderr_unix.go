//go:build !windows

package main

import (
	"bufio"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"syscall"
)

// fd1 (stdout) and fd2 (stderr) are both redirected through pipes; geta's own
// user-facing output (tokens, response bodies, status lines) is sent to the
// SAVED file descriptors so it bypasses the pipes. Anything the SDK / libcurl
// write to either fd is line-scanned and routed through slog at a level
// inferred from the prefix. Blank lines are dropped — the immediate fix for
// the "double newline" libcurl-verbose artefact.
//
// Why both: the SDK's [ERROR]/[INFO]/etc. log channel writes to fd 2; the
// Kotlin/Native `println` used by `curlDebugCallback` writes to fd 1. Without
// fd 1 interception the curl `Tls [...]` chatter leaks to stdout untouched.

var (
	stdoutSavedFile  *os.File
	stdoutPipeReader *os.File
	stderrSavedFile  *os.File
	stderrPipeReader *os.File
	interceptOnce    sync.Once
)

// interceptStderr sets up redirects for both fd 1 and fd 2 so the SDK's
// C-side writes (Kotlin `println` → fprintf(stdout); `[ERROR] …` → stderr;
// libcurl's CURLOPT_VERBOSE) feed into a pipe we pump through slog.
// Returns the writer slog itself should use (the saved fd 2).
//
// To keep Go's own `fmt.Println` / `os.Stdout.Write` going to the real
// terminal — not recursed through the pipe — we ALSO reassign the package
// globals `os.Stdout` and `os.Stderr` to the saved files. Go callers don't
// need to know this happened.
func interceptStderr() io.Writer {
	var w io.Writer = os.Stderr
	interceptOnce.Do(func() {
		stdoutSavedFile, stdoutPipeReader = redirectFD(os.Stdout.Fd(), "/dev/stdout-original")
		stderrSavedFile, stderrPipeReader = redirectFD(os.Stderr.Fd(), "/dev/stderr-original")
		if stdoutSavedFile != nil {
			os.Stdout = stdoutSavedFile
		}
		if stderrSavedFile != nil {
			os.Stderr = stderrSavedFile
			w = stderrSavedFile
		}
	})
	return w
}

// redirectFD dup()s the given fd to a saved file, then dup2()s a fresh pipe's
// write end onto the original fd number. Returns (saved, pipeReader) or
// (nil, nil) on any error — caller should treat both nil as "no interception".
func redirectFD(fd uintptr, savedName string) (*os.File, *os.File) {
	savedFD, err := syscall.Dup(int(fd))
	if err != nil {
		return nil, nil
	}
	saved := os.NewFile(uintptr(savedFD), savedName)
	pr, pw, err := os.Pipe()
	if err != nil {
		_ = saved.Close()
		return nil, nil
	}
	if err := syscall.Dup2(int(pw.Fd()), int(fd)); err != nil {
		_ = saved.Close()
		_ = pr.Close()
		_ = pw.Close()
		return nil, nil
	}
	_ = pw.Close()
	return saved, pr
}

// pumpStderr reads from both pipe ends and routes lines through slog. Source
// tag distinguishes the SDK log channel (fd 2) from curl's println-driven
// debug (fd 1).
func pumpStderr(logger *slog.Logger) {
	if stderrPipeReader != nil {
		go scanThroughSlog(stderrPipeReader, logger, "stderr")
	}
	if stdoutPipeReader != nil {
		go scanThroughSlog(stdoutPipeReader, logger, "stdout")
	}
}

func scanThroughSlog(r *os.File, logger *slog.Logger, defaultSource string) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1<<16), 1<<20)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r\n")
		if line == "" {
			continue
		}
		classifyAndLog(logger, line, defaultSource)
	}
}

func classifyAndLog(logger *slog.Logger, line, defaultSource string) {
	switch {
	case strings.HasPrefix(line, "[ERROR]"):
		logger.Error(strings.TrimSpace(strings.TrimPrefix(line, "[ERROR]")), slog.String("source", "sdk"))
	case strings.HasPrefix(line, "[WARN]"):
		logger.Warn(strings.TrimSpace(strings.TrimPrefix(line, "[WARN]")), slog.String("source", "sdk"))
	case strings.HasPrefix(line, "[INFO]"):
		logger.Info(strings.TrimSpace(strings.TrimPrefix(line, "[INFO]")), slog.String("source", "sdk"))
	case strings.HasPrefix(line, "[DEBUG]"), strings.HasPrefix(line, "[sdk DEBUG]"):
		logger.Debug(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "[DEBUG]"), "[sdk DEBUG]")), slog.String("source", "sdk"))
	case strings.HasPrefix(line, "Tls "):
		logger.Debug(line, slog.String("source", "curl"))
	default:
		logger.Debug(line, slog.String("source", defaultSource))
	}
}
