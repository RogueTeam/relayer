package ioutils

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/klauspost/compress/zstd"
)

type ZstdConn struct {
	once sync.Once
	enc  *zstd.Encoder
	dec  *zstd.Decoder
	net.Conn
}

func (z *ZstdConn) init() {
	z.enc, _ = zstd.NewWriter(
		z.Conn,
		zstd.WithEncoderLevel(zstd.SpeedBestCompression),
	)
	z.dec, _ = zstd.NewReader(z.Conn)
}

func (z *ZstdConn) Read(p []byte) (n int, err error) {
	z.once.Do(z.init)

	return z.dec.Read(p)
}

func (z *ZstdConn) Write(p []byte) (n int, err error) {
	z.once.Do(z.init)

	z.enc.Reset(z.Conn)
	n, err = z.enc.Write(p)
	if err != nil {
		return n, err
	}
	err = z.enc.Flush()
	if err != nil {
		return n, fmt.Errorf("falied to flush encoder: %w", err)
	}

	err = z.enc.Close()
	if err != nil {
		return n, fmt.Errorf("failed to close encoder: %w", err)
	}
	return n, nil
}

func CopyFromZstd(dst io.Writer, src io.Reader) (n int64, err error) {
	dec, err := zstd.NewReader(src)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare zstd reader: %w", err)
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
			return n, fmt.Errorf("failed to read from zstd source: %w", err)
		}

	}
}

func CopyToZstd(dst io.Writer, src io.Reader, level zstd.EncoderLevel) (n int64, err error) {
	enc, err := zstd.NewWriter(dst, zstd.WithEncoderLevel(level))
	if err != nil {
		return 0, fmt.Errorf("failed to prepare zstd writer: %w", err)
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
