package sdktest_test

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gematik/zeta-client-go/internal/sdk"
	"github.com/gematik/zeta-client-go/internal/sdk/sdktest"
)

type memStorage struct {
	mu sync.RWMutex
	m  map[string]string
}

func newMemStorage() *memStorage {
	return &memStorage{m: map[string]string{}}
}

func (s *memStorage) Put(k, v string) error {
	s.mu.Lock()
	s.m[k] = v
	s.mu.Unlock()
	return nil
}

func (s *memStorage) Get(k string) (string, error) {
	s.mu.RLock()
	v, ok := s.m[k]
	s.mu.RUnlock()
	if !ok {
		return "", sdk.ErrNotFound
	}
	return v, nil
}

func (s *memStorage) Remove(k string) error {
	s.mu.Lock()
	delete(s.m, k)
	s.mu.Unlock()
	return nil
}

func (s *memStorage) Clear() error {
	s.mu.Lock()
	s.m = map[string]string{}
	s.mu.Unlock()
	return nil
}

func TestStorageRoundtrip(t *testing.T) {
	sh := sdk.NewStorageHandle(newMemStorage())
	defer sh.Close()

	if c := sdktest.InvokeGet(sh, "k1"); c.OK || c.Fired.Load() != 1 {
		t.Fatalf("get empty: ok=%v fired=%d", c.OK, c.Fired.Load())
	}
	if c := sdktest.InvokePut(sh, "k1", "v1"); c.Fired.Load() != 1 {
		t.Fatalf("put: fired=%d", c.Fired.Load())
	}
	if c := sdktest.InvokeGet(sh, "k1"); !c.OK || c.Value != "v1" || c.Fired.Load() != 1 {
		t.Fatalf("get: ok=%v value=%q fired=%d", c.OK, c.Value, c.Fired.Load())
	}
	if c := sdktest.InvokeRemove(sh, "k1"); c.Fired.Load() != 1 {
		t.Fatalf("remove: fired=%d", c.Fired.Load())
	}
	if c := sdktest.InvokeGet(sh, "k1"); c.OK {
		t.Fatalf("get after remove: should be missing")
	}
	sdktest.InvokePut(sh, "k2", "v2")
	sdktest.InvokePut(sh, "k3", "v3")
	if c := sdktest.InvokeClear(sh); c.Fired.Load() != 1 {
		t.Fatalf("clear: fired=%d", c.Fired.Load())
	}
	if c := sdktest.InvokeGet(sh, "k2"); c.OK {
		t.Fatalf("get after clear: should be missing")
	}
}

// installCleanupHook routes sdk.TestCleanupHook into a buffered channel for
// the duration of the test. Tests must not run in parallel while it is set.
func installCleanupHook(t *testing.T) (fires <-chan struct{}, count *atomic.Int32) {
	t.Helper()
	ch := make(chan struct{}, 1024)
	c := &atomic.Int32{}
	sdk.TestCleanupHook = func() {
		c.Add(1)
		ch <- struct{}{}
	}
	t.Cleanup(func() { sdk.TestCleanupHook = nil })
	return ch, c
}

func TestStorageHandleCloseIdempotent(t *testing.T) {
	_, count := installCleanupHook(t)

	sh := sdk.NewStorageHandle(newMemStorage())
	sh.Close()
	sh.Close()
	sh.Close()

	if got := count.Load(); got != 1 {
		t.Fatalf("expected exactly 1 cleanup invocation, got %d", got)
	}
}

func TestStorageHandleCleanupOnGC(t *testing.T) {
	fires, count := installCleanupHook(t)

	const n = 10
	for range n {
		_ = sdk.NewStorageHandle(newMemStorage())
	}

	deadline := time.After(5 * time.Second)
	for got := 0; got < n; {
		runtime.GC()
		select {
		case <-fires:
			got++
		case <-time.After(50 * time.Millisecond):
		case <-deadline:
			t.Fatalf("only %d/%d cleanups fired (count=%d)", got, n, count.Load())
		}
	}

	if got := count.Load(); got != n {
		t.Fatalf("expected exactly %d cleanups, got %d", n, got)
	}
}

// captureLogger is a Logger that records every Log call. Concurrent-safe.
type captureLogger struct {
	mu      sync.Mutex
	entries []logEntry
}

type logEntry struct {
	Level   int
	Tag     string
	Message string
}

func (c *captureLogger) Log(level int, tag, message string) {
	c.mu.Lock()
	c.entries = append(c.entries, logEntry{level, tag, message})
	c.mu.Unlock()
}

func (c *captureLogger) Entries() []logEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]logEntry, len(c.entries))
	copy(out, c.entries)
	return out
}

func TestLoggerCallback(t *testing.T) {
	cap := &captureLogger{}
	lh := sdk.NewLoggerHandle(cap)
	defer lh.Close()

	cases := []struct {
		level, tag, message string
		wantLevel           int
	}{
		{"DEBUG", "net", "connecting", sdk.LogLevelDebug},
		{"INFO", "auth", "token refreshed", sdk.LogLevelInfo},
		{"WARN", "", "deprecated header", sdk.LogLevelWarn},
		{"ERROR", "io", "connection lost", sdk.LogLevelError},
		{"UNKNOWN", "x", "garbage", sdk.LogLevelInfo}, // unknown maps to Info
	}
	for _, c := range cases {
		sdktest.InvokeLog(lh, c.level, c.tag, c.message)
	}

	got := cap.Entries()
	if len(got) != len(cases) {
		t.Fatalf("expected %d entries, got %d", len(cases), len(got))
	}
	for i, c := range cases {
		if got[i].Level != c.wantLevel || got[i].Tag != c.tag || got[i].Message != c.message {
			t.Errorf("entry %d: got %+v, want level=%d tag=%q msg=%q",
				i, got[i], c.wantLevel, c.tag, c.message)
		}
	}
}

func TestLoggerHandleCloseIdempotent(t *testing.T) {
	_, count := installCleanupHook(t)

	lh := sdk.NewLoggerHandle(&captureLogger{})
	lh.Close()
	lh.Close()
	lh.Close()

	if got := count.Load(); got != 1 {
		t.Fatalf("expected exactly 1 cleanup invocation, got %d", got)
	}
}

func TestLoggerHandleCleanupOnGC(t *testing.T) {
	fires, count := installCleanupHook(t)

	const n = 10
	for range n {
		_ = sdk.NewLoggerHandle(&captureLogger{})
	}

	deadline := time.After(5 * time.Second)
	for got := 0; got < n; {
		runtime.GC()
		select {
		case <-fires:
			got++
		case <-time.After(50 * time.Millisecond):
		case <-deadline:
			t.Fatalf("only %d/%d cleanups fired (count=%d)", got, n, count.Load())
		}
	}
	if got := count.Load(); got != n {
		t.Fatalf("expected exactly %d cleanups, got %d", n, got)
	}
}

type stubConnector struct {
	cert      []byte
	signature []byte
	err       error
	lastChall string
}

func (s *stubConnector) ReadCertificate() ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.cert, nil
}

func (s *stubConnector) ExternalAuthenticate(challenge string) ([]byte, error) {
	s.lastChall = challenge
	if s.err != nil {
		return nil, s.err
	}
	return s.signature, nil
}

func TestSMCBReadCertificate(t *testing.T) {
	stub := &stubConnector{cert: []byte{0x30, 0x82, 0xde, 0xad, 0xbe, 0xef}}
	sh := sdk.NewSmcbHandle(stub)
	defer sh.Close()

	c := sdktest.InvokeReadCertificate(sh)
	if c.Fired.Load() != 1 {
		t.Fatalf("fired=%d", c.Fired.Load())
	}
	if !bytes.Equal(c.Bytes, stub.cert) {
		t.Fatalf("cert mismatch: got %x, want %x", c.Bytes, stub.cert)
	}
}

func TestSMCBExternalAuthenticate(t *testing.T) {
	stub := &stubConnector{signature: []byte{0xca, 0xfe, 0xba, 0xbe}}
	sh := sdk.NewSmcbHandle(stub)
	defer sh.Close()

	const chall = "dGVzdC1jaGFsbGVuZ2U="
	c := sdktest.InvokeExternalAuthenticate(sh, chall)
	if c.Fired.Load() != 1 {
		t.Fatalf("fired=%d", c.Fired.Load())
	}
	if !bytes.Equal(c.Bytes, stub.signature) {
		t.Fatalf("signature mismatch: got %x, want %x", c.Bytes, stub.signature)
	}
	if stub.lastChall != chall {
		t.Fatalf("challenge not propagated: got %q, want %q", stub.lastChall, chall)
	}
}

func TestSMCBReadCertificate_ErrorReturnsEmpty(t *testing.T) {
	stub := &stubConnector{err: errors.New("simulated card failure")}
	sh := sdk.NewSmcbHandle(stub)
	defer sh.Close()

	c := sdktest.InvokeReadCertificate(sh)
	if c.Fired.Load() != 1 {
		t.Fatalf("fired=%d (callback must fire exactly once even on error)", c.Fired.Load())
	}
	if len(c.Bytes) != 0 {
		t.Fatalf("expected empty bytes on error, got %x", c.Bytes)
	}
}

func TestSMCBExternalAuthenticate_ErrorReturnsEmpty(t *testing.T) {
	stub := &stubConnector{err: errors.New("simulated card failure")}
	sh := sdk.NewSmcbHandle(stub)
	defer sh.Close()

	c := sdktest.InvokeExternalAuthenticate(sh, "anything")
	if c.Fired.Load() != 1 {
		t.Fatalf("fired=%d", c.Fired.Load())
	}
	if len(c.Bytes) != 0 {
		t.Fatalf("expected empty bytes on error, got %x", c.Bytes)
	}
}

func TestSMCBHandleCloseIdempotent(t *testing.T) {
	_, count := installCleanupHook(t)

	sh := sdk.NewSmcbHandle(&stubConnector{})
	sh.Close()
	sh.Close()
	sh.Close()

	if got := count.Load(); got != 1 {
		t.Fatalf("expected exactly 1 cleanup invocation, got %d", got)
	}
}

func TestSMCBHandleCleanupOnGC(t *testing.T) {
	fires, count := installCleanupHook(t)

	const n = 10
	for range n {
		_ = sdk.NewSmcbHandle(&stubConnector{})
	}

	deadline := time.After(5 * time.Second)
	for got := 0; got < n; {
		runtime.GC()
		select {
		case <-fires:
			got++
		case <-time.After(50 * time.Millisecond):
		case <-deadline:
			t.Fatalf("only %d/%d cleanups fired (count=%d)", got, n, count.Load())
		}
	}
	if got := count.Load(); got != n {
		t.Fatalf("expected exactly %d cleanups, got %d", n, got)
	}
}

func TestStorageStress(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test skipped under -short")
	}
	sh := sdk.NewStorageHandle(newMemStorage())
	defer sh.Close()

	const iterations = 10_000
	const workers = 8
	var wg sync.WaitGroup
	for g := range workers {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := range iterations {
				key := fmt.Sprintf("g%d-k%d", g, i)
				if c := sdktest.InvokePut(sh, key, "v"); c.Fired.Load() != 1 {
					t.Errorf("put fired=%d", c.Fired.Load())
					return
				}
				if c := sdktest.InvokeGet(sh, key); !c.OK || c.Fired.Load() != 1 {
					t.Errorf("get ok=%v fired=%d", c.OK, c.Fired.Load())
					return
				}
				if c := sdktest.InvokeRemove(sh, key); c.Fired.Load() != 1 {
					t.Errorf("remove fired=%d", c.Fired.Load())
					return
				}
			}
		}(g)
	}
	wg.Wait()
}
