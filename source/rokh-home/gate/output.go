package gate

import "sync"

const outputLimit = 64 << 10

// outputTail is a bounded, in-memory writer for untrusted program output.
// The owner receives a truncation flag when earlier bytes were discarded.
type outputTail struct {
	mu        sync.Mutex
	buf       []byte
	truncated bool
}

func (w *outputTail) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	if len(w.buf)+n > outputLimit {
		w.truncated = true
	}
	if n >= outputLimit {
		w.buf = append(w.buf[:0], p[n-outputLimit:]...)
	} else {
		if excess := len(w.buf) + n - outputLimit; excess > 0 {
			copy(w.buf, w.buf[excess:])
			w.buf = w.buf[:len(w.buf)-excess]
		}
		w.buf = append(w.buf, p...)
	}
	return n, nil
}

func (w *outputTail) snapshot() (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf), w.truncated
}
