package ioutils

import (
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/gzip"
)

const BufferSize = 32 * 1024

func CopyFromGzip(dst io.Writer, src io.Reader) (n int64, err error) {
	dec, err := gzip.NewReader(src)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare gzip reader: %w", err)
	}

	var buffer = make([]byte, BufferSize)
	for {
		readN, err := dec.Read(buffer)
		n += int64(readN)
		if readN > 0 {
			_, err := dst.Write(buffer[:readN])
			if err != nil {
				return n, fmt.Errorf("failed to write contents: %w", err)
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				return n, nil
			}
			return n, fmt.Errorf("failed to read from gzip source: %w", err)
		}

	}
}

func CopyToGzip(dst io.Writer, src io.Reader, level int) (n int64, err error) {
	enc, err := gzip.NewWriterLevel(dst, level)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare gzip writer: %w", err)
	}

	var buffer = make([]byte, BufferSize)
	for {
		readN, err := src.Read(buffer)
		n += int64(readN)

		if readN > 0 {
			enc.Reset(dst)

			_, err := enc.Write(buffer[:readN])
			if err != nil {
				return n, fmt.Errorf("failed to write contents: %w", err)
			}

			err = enc.Flush()
			if err != nil {
				return n, fmt.Errorf("falied to flush encoder: %w", err)
			}

			err = enc.Close()
			if err != nil {
				return n, fmt.Errorf("failed to close encoder: %w", err)
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				return n, nil
			}
			return n, fmt.Errorf("failed to read from plain source: %w", err)
		}
	}
}
