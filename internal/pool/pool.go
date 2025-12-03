package pool

import "sync"

type Pool[T any] struct {
	pool sync.Pool
}

func (p *Pool[T]) Get() (v T) {
	return p.pool.New().(T)
}

func (p *Pool[T]) Put(v T) {
	p.pool.Put(v)
}

func New[T any](f func() (v T)) (p *Pool[T]) {
	return &Pool[T]{
		pool: sync.Pool{
			New: func() any {
				return f()
			},
		},
	}
}
