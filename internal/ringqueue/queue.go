package ringqueue

import (
	"errors"
	"sync"
)

type Queue[T any] struct {
	mutex  *sync.Mutex
	idx    int
	values []T
}

func (q *Queue[T]) Empty() (empty bool) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	return len(q.values) == 0
}

func (q *Queue[T]) Set(vs []T) (err error) {
	if len(vs) == 0 {
		return errors.New("no values provided")
	}

	q.mutex.Lock()
	defer q.mutex.Unlock()

	q.idx = 0
	q.values = vs
	return nil
}

func (q *Queue[T]) Next() (v T) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if q.idx >= len(q.values) {
		q.idx = 0
	}
	v = q.values[q.idx]
	q.idx++
	return v
}

// If len of s is zero. it returns an error
func New[T any]() (q *Queue[T]) {
	q = &Queue[T]{
		mutex: new(sync.Mutex),
	}
	return q
}
