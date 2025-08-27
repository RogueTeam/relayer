package ringqueue_test

import (
	"testing"

	"github.com/RogueTeam/relayer/internal/ringqueue"
	"github.com/stretchr/testify/assert"
)

func Test_Queue(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {

		type Test struct {
			Name   string
			Slice  []any
			Pops   int
			Expect any
		}
		tests := []Test{
			{Name: "3 Pops", Slice: []any{0, 1, 2}, Pops: 3, Expect: 2},
			{Name: "3 Pops, smaller queue", Slice: []any{0, 1}, Pops: 3, Expect: 0},
			{Name: "6 Pops, smaller queue", Slice: []any{0, 1}, Pops: 6, Expect: 1},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				assertions := assert.New(t)

				q, err := ringqueue.New(test.Slice)
				assertions.Nil(err, "expecting no error")

				for range test.Pops - 1 {
					q.Next()
				}
				v := q.Next()
				assertions.Equal(test.Expect, v, "expecting different value")
			})
		}
	})
	t.Run("Fail", func(t *testing.T) {

		type Test struct {
			Name  string
			Slice []any
		}
		tests := []Test{
			{Name: "Empty slice"},
		}

		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				assertions := assert.New(t)

				q, err := ringqueue.New(test.Slice)
				assertions.NotNil(err, "expecting error")
				assertions.Nil(q, "expecting no queue")
			})
		}
	})
}
