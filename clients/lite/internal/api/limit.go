package api

import (
	"context"
	"io"

	"golang.org/x/time/rate"
)

// Limiter caps throughput in bytes per second. Its rate can change while
// transfers run, so the settings page takes effect without a restart.
type Limiter struct {
	inner *rate.Limiter
}

// chunk is the most a single read waits for at once; it is also the bucket
// size, so a burst never exceeds a fraction of a second at low limits.
const chunk = 32 << 10

// NewLimiter returns a limiter; bytesPerSecond <= 0 means unlimited.
func NewLimiter(bytesPerSecond int64) *Limiter {
	l := &Limiter{inner: rate.NewLimiter(rate.Inf, chunk)}
	l.SetRate(bytesPerSecond)
	return l
}

// SetRate changes the limit; <= 0 removes it.
func (l *Limiter) SetRate(bytesPerSecond int64) {
	if bytesPerSecond <= 0 {
		l.inner.SetLimit(rate.Inf)
		return
	}
	l.inner.SetLimit(rate.Limit(bytesPerSecond))
}

// Reader wraps r so reads wait for the limiter. A nil limiter passes r through.
func (l *Limiter) Reader(ctx context.Context, r io.Reader) io.Reader {
	if l == nil {
		return r
	}
	return &limitedReader{ctx: ctx, r: r, l: l.inner}
}

type limitedReader struct {
	ctx context.Context
	r   io.Reader
	l   *rate.Limiter
}

func (lr *limitedReader) Read(p []byte) (int, error) {
	if len(p) > chunk {
		p = p[:chunk]
	}
	n, err := lr.r.Read(p)
	if n > 0 {
		if waitErr := lr.l.WaitN(lr.ctx, n); waitErr != nil {
			return n, waitErr
		}
	}
	return n, err
}
