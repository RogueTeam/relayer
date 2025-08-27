package ringqueue

import "errors"

type Queue[T any] struct {
	idx    int
	values []T
}

func (q *Queue[T]) Next() (v T) {
	if q.idx >= len(q.values) {
		q.idx = 0
	}
	v = q.values[q.idx]
	q.idx++
	return v
}

// If len of s is zero. it returns an error
func New[T any](s []T) (q *Queue[T], err error) {
	if len(s) == 0 {
		return nil, errors.New("cannot receive empty slice")
	}

	q = &Queue[T]{
		values: s,
	}
	return q, nil
}
