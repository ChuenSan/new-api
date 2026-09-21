package helper

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newBufferedTestWriter(t *testing.T, limitMB int) (*BufferedWriter, *httptest.ResponseRecorder) {
	t.Helper()
	original := constant.StreamScannerMaxBufferMB
	constant.StreamScannerMaxBufferMB = limitMB
	t.Cleanup(func() { constant.StreamScannerMaxBufferMB = original })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	return NewStreamBufferedWriter(c.Writer), w
}

func TestBufferedWriterHoldsOutputUntilCommit(t *testing.T) {
	buf, rec := newBufferedTestWriter(t, 0)

	_, err := buf.WriteString("data: first\n\n")
	require.NoError(t, err)
	buf.Flush()
	assert.False(t, buf.Committed())
	assert.Empty(t, rec.Body.String(), "buffered output must not reach the client before commit")

	buf.Commit()
	assert.True(t, buf.Committed())
	assert.Equal(t, "data: first\n\n", rec.Body.String())

	_, err = buf.WriteString("data: second\n\n")
	require.NoError(t, err)
	assert.Equal(t, "data: first\n\ndata: second\n\n", rec.Body.String(), "post-commit output passes through")
}

func TestBufferedWriterKeepAlivePassesThrough(t *testing.T) {
	buf, rec := newBufferedTestWriter(t, 0)

	_, err := buf.WriteString(sseKeepAlivePrefix + "\n\n")
	require.NoError(t, err)
	_, err = buf.WriteString("data: body\n\n")
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(rec.Body.String(), sseKeepAlivePrefix))
	assert.NotContains(t, rec.Body.String(), "data: body")
}

func TestBufferedWriterDiscardDropsBufferAndPassesThrough(t *testing.T) {
	buf, rec := newBufferedTestWriter(t, 0)

	_, err := buf.WriteString("data: discarded\n\n")
	require.NoError(t, err)
	buf.Discard()
	assert.False(t, buf.Committed())
	assert.Empty(t, rec.Body.String())

	_, err = buf.WriteString(`{"error":"failed"}`)
	require.NoError(t, err)
	assert.Equal(t, `{"error":"failed"}`, rec.Body.String())
}

func TestBufferedWriterCommitAndDiscardAreIdempotent(t *testing.T) {
	buf, rec := newBufferedTestWriter(t, 0)

	_, err := buf.WriteString("data: kept\n\n")
	require.NoError(t, err)
	buf.Commit()
	buf.Commit()
	buf.Discard()

	assert.Equal(t, "data: kept\n\n", rec.Body.String())
}

func TestBufferedWriterOversizeDegradesToPassthrough(t *testing.T) {
	buf, rec := newBufferedTestWriter(t, 1) // 1MB limit

	chunk := strings.Repeat("a", 600*1024)
	_, err := buf.WriteString(chunk)
	require.NoError(t, err)
	assert.Empty(t, rec.Body.String())

	_, err = buf.WriteString(chunk)
	require.NoError(t, err)
	assert.True(t, buf.Committed(), "overflow releases the buffer instead of failing the request")
	assert.Equal(t, len(chunk)*2, len(rec.Body.String()))
}
