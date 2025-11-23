package ioutils_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/RogueTeam/relayer/internal/ioutils"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
)

func Test_ZstdCopy(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type Test struct {
			Repeat int
		}
		tests := []Test{
			{Repeat: 1},
			{Repeat: 10},
			{Repeat: 100},
			{Repeat: 1_000},
			{Repeat: 100_000},
			{Repeat: 1_000_000},
			{Repeat: 100_000_000},
		}
		for _, test := range tests {
			t.Run(fmt.Sprintf("Repeat %d", test.Repeat), func(t *testing.T) {
				assertions := assert.New(t)

				var payload = []byte(strings.Repeat("Hello world!!!\n", test.Repeat))

				var compressBuffer bytes.Buffer
				_, err := ioutils.CopyToZstd(&compressBuffer, bytes.NewReader(payload), zstd.SpeedBestCompression)
				if !assertions.Nil(err, "failed to copy to zstd") {
					return
				}

				var dataBuffer bytes.Buffer
				_, err = ioutils.CopyFromZstd(&dataBuffer, &compressBuffer)
				if !assertions.Nil(err, "failed to decompress data") {
					return
				}

				if !assertions.Equal(payload, dataBuffer.Bytes(), "contents doesn't match") {
					return
				}
			})
		}
	})
}
