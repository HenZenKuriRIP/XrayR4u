package limiter

import (
	"github.com/juju/ratelimit"
	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
)

// Writer wraps a buf.Writer and throttles writes through a token-bucket limiter.
type Writer struct {
	writer  buf.Writer
	limiter *ratelimit.Bucket
}

// RateWriter wraps w with the given token-bucket limiter.
func (l *Limiter) RateWriter(writer buf.Writer, limiter *ratelimit.Bucket) buf.Writer {
	return &Writer{
		writer:  writer,
		limiter: limiter,
	}
}

// Reader wraps a buf.Reader and throttles reads through a token-bucket limiter.
type Reader struct {
	reader  buf.Reader
	limiter *ratelimit.Bucket
}

// RateReader wraps r with the given token-bucket limiter.
func (l *Limiter) RateReader(reader buf.Reader, limiter *ratelimit.Bucket) buf.Reader {
	return &Reader{
		reader:  reader,
		limiter: limiter,
	}
}

// ReadMultiBuffer blocks until the token bucket allows the read, then
// forwards the buffer from the underlying reader.
func (r *Reader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	mb, err := r.reader.ReadMultiBuffer()
	if mb != nil && mb.Len() > 0 {
		r.limiter.Wait(int64(mb.Len()))
	}
	return mb, err
}

// Close closes the underlying writer.
func (w *Writer) Close() error {
	return common.Close(w.writer)
}

// WriteMultiBuffer blocks until the token bucket allows the write, then
// forwards the buffer to the underlying writer.
func (w *Writer) WriteMultiBuffer(mb buf.MultiBuffer) error {
	w.limiter.Wait(int64(mb.Len()))
	return w.writer.WriteMultiBuffer(mb)
}
