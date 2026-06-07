//go:build windows

package main

// Windows equivalent of the unix fd-redirect dance. The complication: on
// Windows the K/N runtime's `println()` writes via msvcrt's `stdout` FILE*,
// which caches the OS HANDLE at CRT init time. `SetStdHandle` only updates
// what `GetStdHandle` returns — it does NOT touch the cached FILE* in
// msvcrt. So we have to call msvcrt's own `_dup` / `_dup2` from Go to swap
// the fd 1 / fd 2 that msvcrt itself sees.
//
// Win32 CreatePipe gives us OS HANDLEs; `_open_osfhandle` turns those into
// CRT fds; `_dup2` rewires fd 1 / 2 at the CRT level.

import (
	"bufio"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var (
	stdoutSavedFile  *os.File
	stdoutPipeReader *os.File
	stderrSavedFile  *os.File
	stderrPipeReader *os.File
	interceptOnce    sync.Once
)

var (
	msvcrt = syscall.NewLazyDLL("msvcrt.dll")

	procDup           = msvcrt.NewProc("_dup")
	procDup2          = msvcrt.NewProc("_dup2")
	procOpenOSFHandle = msvcrt.NewProc("_open_osfhandle")
	procGetOSFHandle  = msvcrt.NewProc("_get_osfhandle")
	procClose         = msvcrt.NewProc("_close")

	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procCreatePipe   = kernel32.NewProc("CreatePipe")
	procSetStdHandle = kernel32.NewProc("SetStdHandle")
)

const (
	_O_RDONLY = 0x0000
	_O_APPEND = 0x0008
	_O_TEXT   = 0x4000
)

func crtDup(fd int32) int32 {
	r, _, _ := procDup.Call(uintptr(fd))
	return int32(r)
}

func crtDup2(src, dst int32) int32 {
	r, _, _ := procDup2.Call(uintptr(src), uintptr(dst))
	return int32(r)
}

func crtOpenOSFHandle(h syscall.Handle, flags int) int32 {
	r, _, _ := procOpenOSFHandle.Call(uintptr(h), uintptr(flags))
	return int32(r)
}

func crtGetOSFHandle(fd int32) syscall.Handle {
	r, _, _ := procGetOSFHandle.Call(uintptr(fd))
	return syscall.Handle(r)
}

func interceptStderr() io.Writer {
	var w io.Writer = os.Stderr
	interceptOnce.Do(func() {
		stdoutSavedFile, stdoutPipeReader = redirectCRTFD(1, "/dev/stdout-original")
		stderrSavedFile, stderrPipeReader = redirectCRTFD(2, "/dev/stderr-original")
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

// redirectCRTFD saves the current fd `target` (CRT-level), then rewires it to
// a fresh pipe write end. Returns (saved-fd-as-File, pipe-read-end-as-File).
func redirectCRTFD(target int32, savedName string) (*os.File, *os.File) {
	// Save the current CRT fd.
	savedCRTFD := crtDup(target)
	if savedCRTFD < 0 {
		return nil, nil
	}
	savedOSHandle := crtGetOSFHandle(savedCRTFD)
	saved := os.NewFile(uintptr(savedOSHandle), savedName)
	if saved == nil {
		return nil, nil
	}

	// Make a pipe at the OS HANDLE level.
	var readHandle, writeHandle syscall.Handle
	r, _, _ := procCreatePipe.Call(
		uintptr(unsafe.Pointer(&readHandle)),
		uintptr(unsafe.Pointer(&writeHandle)),
		0,
		0,
	)
	if r == 0 {
		_ = saved.Close()
		return nil, nil
	}

	// Wrap the pipe write end as a CRT fd, then dup2 it onto `target` so
	// msvcrt's stdout/stderr FILE* now writes into our pipe.
	writeCRTFD := crtOpenOSFHandle(writeHandle, _O_APPEND|_O_TEXT)
	if writeCRTFD < 0 {
		_ = syscall.CloseHandle(readHandle)
		_ = syscall.CloseHandle(writeHandle)
		_ = saved.Close()
		return nil, nil
	}
	if crtDup2(writeCRTFD, target) < 0 {
		_, _, _ = procClose.Call(uintptr(writeCRTFD))
		_ = syscall.CloseHandle(readHandle)
		_ = saved.Close()
		return nil, nil
	}
	// dup2 made `target` an alias for the pipe write end. Close our copy.
	_, _, _ = procClose.Call(uintptr(writeCRTFD))

	// Also reflect the change at the Win32 layer so any code using
	// GetStdHandle (rather than the CRT) sees the pipe too. The std-handle
	// constants are negative (STD_OUTPUT_HANDLE=-11, STD_ERROR_HANDLE=-12)
	// but the Win32 API takes them as DWORD (unsigned), so we cast.
	var stdConstant int32
	switch target {
	case 1:
		stdConstant = -11
	case 2:
		stdConstant = -12
	}
	if stdConstant != 0 {
		procSetStdHandle.Call(uintptr(uint32(stdConstant)), uintptr(writeHandle))
	}

	return saved, os.NewFile(uintptr(readHandle), "pipe-read-end")
}

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
		classifyAndLogWin(logger, line, defaultSource)
	}
}

func classifyAndLogWin(logger *slog.Logger, line, defaultSource string) {
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
