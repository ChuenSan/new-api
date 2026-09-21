package helper

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
)

// sseKeepAlivePrefix is the SSE comment written by PingData. Keep-alives bypass
// the buffer so a long upstream attempt still keeps the client connection alive.
const sseKeepAlivePrefix = ": PING"

// BufferedWriter holds relay output in memory until Commit, so an attempt that
// produced no usable response can be discarded without ever reaching the
// client (availability mode). Writes made before Commit are buffered, except
// keep-alive comments and output that exceeds the process-wide stream buffer
// limit; after Commit or Discard everything goes straight through.
type BufferedWriter struct {
	gin.ResponseWriter

	buf       bytes.Buffer
	limit     int
	committed bool
	detached  bool
}

// NewStreamBufferedWriter wraps w with the process-wide stream buffer limit.
func NewStreamBufferedWriter(w gin.ResponseWriter) *BufferedWriter {
	limit := 0
	if constant.StreamScannerMaxBufferMB > 0 {
		limit = constant.StreamScannerMaxBufferMB << 20
	}
	return &BufferedWriter{ResponseWriter: w, limit: limit}
}

// Committed reports whether the buffered output was released to the client.
func (w *BufferedWriter) Committed() bool { return w.committed }

// Commit releases the buffered output. It is idempotent, and after it the
// writer stops buffering because those bytes can no longer be retracted.
func (w *BufferedWriter) Commit() {
	if w.passthrough() {
		return
	}
	w.committed = true
	w.ResponseWriter.WriteHeaderNow()
	_, _ = w.buf.WriteTo(w.ResponseWriter)
	w.ResponseWriter.Flush()
}

// Discard drops the buffered output and stops buffering, so later writes such
// as the final error body reach the client directly.
func (w *BufferedWriter) Discard() {
	if w.passthrough() {
		return
	}
	w.detached = true
	w.buf.Reset()
}

// Unwrap exposes the wrapped writer to http.NewResponseController.
func (w *BufferedWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *BufferedWriter) Write(p []byte) (int, error) {
	if w.passthrough() || bytes.HasPrefix(p, []byte(sseKeepAlivePrefix)) || !w.grow(len(p)) {
		return w.ResponseWriter.Write(p)
	}
	return w.buf.Write(p)
}

func (w *BufferedWriter) WriteString(s string) (int, error) {
	if w.passthrough() || strings.HasPrefix(s, sseKeepAlivePrefix) || !w.grow(len(s)) {
		return w.ResponseWriter.WriteString(s)
	}
	return w.buf.WriteString(s)
}

// Flush keeps buffered output in memory; only a passthrough writer flushes.
func (w *BufferedWriter) Flush() {
	if w.passthrough() {
		w.ResponseWriter.Flush()
	}
}

// WriteHeaderNow is deferred until Commit so status and headers are written
// only when the buffered output is actually released.
func (w *BufferedWriter) WriteHeaderNow() {
	if w.passthrough() {
		w.ResponseWriter.WriteHeaderNow()
	}
}

func (w *BufferedWriter) passthrough() bool { return w.committed || w.detached }

// grow reports whether p still fits the buffer. Oversized output degrades to
// passthrough instead of failing the request, which releases whatever was
// buffered.
func (w *BufferedWriter) grow(n int) bool {
	if w.limit > 0 && w.buf.Len()+n > w.limit {
		w.Commit()
		return false
	}
	return true
}
