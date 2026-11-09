package server

import "context"

type sourceBlock struct {
	index int64
	data  []byte
	hash  string
	err   error
}

// The unbuffered handoff lets the producer read exactly one block ahead of the
// block owned by the HTTP request. Buffers are never pooled or reused by a later
// request; http.Transport may still own a request body after an early reply.
func prefetchBlocks(ctx context.Context, first, end int64, read func(context.Context, int64) ([]byte, string, error)) (<-chan sourceBlock, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	out := make(chan sourceBlock)
	go func() {
		defer close(out)
		for i := first; i < end; i++ {
			if ctx.Err() != nil {
				return
			}
			b, h, err := read(ctx, i)
			select {
			case out <- sourceBlock{i, b, h, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return out, cancel
}
