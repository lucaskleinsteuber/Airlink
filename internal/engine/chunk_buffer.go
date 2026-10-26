package engine

import (
	"context"
	"io"

	"github.com/lucasfd3/Airlink/internal/model"
)

// Block lengths come from the validated manifest. Allocating that exact size
// avoids ReadAll's growth allocations and copying on every large block.
func readChunkBytes(ctx context.Context, reader io.Reader, size int64) ([]byte, error) {
	if size <= 0 || size > model.ChunkSize {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	buffer := make([]byte, int(size))
	reader = contextReader{ctx, reader}
	for offset := 0; offset < len(buffer); {
		n, err := reader.Read(buffer[offset:])
		offset += n
		if err != nil {
			if err == io.EOF {
				if offset == len(buffer) {
					return buffer, nil
				}
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
	}
	return buffer, ctx.Err()
}

// Receive bodies must contain exactly the manifest's block length, including
// when an authenticated caller uses a chunked request without Content-Length.
func requireChunkEOF(ctx context.Context, reader io.Reader) error {
	var extra [1]byte
	reader = contextReader{ctx, reader}
	for {
		n, err := reader.Read(extra[:])
		if err != nil && err != io.EOF {
			return err
		}
		if n != 0 {
			return ErrInvalid
		}
		if err == io.EOF {
			return nil
		}
	}
}
