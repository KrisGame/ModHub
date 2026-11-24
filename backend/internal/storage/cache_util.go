package storage

import (
	"container/list"
	"context"
	"fmt"
	"sync"
	"time"
)

type lruItem[V any] struct {
	key       string
	val       V
	expiresAt int64
}

type LRUCache[V any] struct {
	mu      sync.Mutex
	maxSize int
	ll      *list.List
	items   map[string]*list.Element
}

func NewLRUCache[V any](maxSize int) *LRUCache[V] {
	if maxSize < 16 {
		maxSize = 64
	}
	return &LRUCache[V]{
		maxSize: maxSize,
		ll:      list.New(),
		items:   make(map[string]*list.Element, maxSize),
	}
}

func (c *LRUCache[V]) Get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var zero V
	el, ok := c.items[key]
	if !ok {
		return zero, false
	}
	entry := el.Value.(*lruItem[V])
	if entry.expiresAt > 0 && entry.expiresAt <= time.Now().Unix() {
		c.ll.Remove(el)
		delete(c.items, key)
		return zero, false
	}
	c.ll.MoveToFront(el)
	return entry.val, true
}

func (c *LRUCache[V]) Set(key string, val V, ttlSecs int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var exp int64
	if ttlSecs > 0 {
		exp = time.Now().Unix() + ttlSecs
	}
	if el, ok := c.items[key]; ok {
		entry := el.Value.(*lruItem[V])
		entry.val = val
		entry.expiresAt = exp
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(&lruItem[V]{key: key, val: val, expiresAt: exp})
	c.items[key] = el
	for c.ll.Len() > c.maxSize {
		back := c.ll.Back()
		if back == nil {
			break
		}
		c.ll.Remove(back)
		delete(c.items, back.Value.(*lruItem[V]).key)
	}
}

func (c *LRUCache[V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ll.Init()
	c.items = make(map[string]*list.Element, c.maxSize)
}

// SingleFlight deduplicates concurrent identical requests under the same key.
type sfCall[V any] struct {
	done chan struct{}
	val  V
	err  error
}

type SingleFlight[V any] struct {
	mu    sync.Mutex
	calls map[string]*sfCall[V]
}

func NewSingleFlight[V any]() *SingleFlight[V] {
	return &SingleFlight[V]{calls: make(map[string]*sfCall[V])}
}

func (g *SingleFlight[V]) Do(key string, fn func() (V, error)) (V, error) {
	return g.DoCtx(context.Background(), key, fn)
}

func (g *SingleFlight[V]) DoCtx(ctx context.Context, key string, fn func() (V, error)) (V, error) {
	var zero V
	if ctx != nil && ctx.Err() != nil {
		return zero, ctx.Err()
	}

	g.mu.Lock()
	if existing, ok := g.calls[key]; ok {
		g.mu.Unlock()
		if ctx == nil {
			<-existing.done
			return existing.val, existing.err
		}
		select {
		case <-existing.done:
			return existing.val, existing.err
		case <-ctx.Done():
			return zero, ctx.Err()
		}
	}
	c := &sfCall[V]{done: make(chan struct{})}
	g.calls[key] = c
	g.mu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.err = fmt.Errorf("singleflight panic: %v", r)
			}
			g.mu.Lock()
			delete(g.calls, key)
			g.mu.Unlock()
			close(c.done)
		}()
		c.val, c.err = fn()
	}()

	if ctx == nil {
		<-c.done
		return c.val, c.err
	}
	select {
	case <-c.done:
		return c.val, c.err
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}
