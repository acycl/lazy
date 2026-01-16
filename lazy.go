// Package lazy provides a generic, context-aware lazy initialization primitive.
package lazy

import (
	"context"
	"sync/atomic"
)

// Func wraps f so that it executes at most once successfully. Subsequent calls
// return the cached result. If f returns an error, future calls will retry.
// The returned function respects context cancellation while waiting to execute f.
func Func[T any](f func(context.Context) (T, error)) func(context.Context) (T, error) {
	// Use a struct so that there's a single heap allocation.
	d := struct {
		f     func(context.Context) (T, error)
		done  atomic.Bool
		sem   chan struct{}
		value T
	}{
		f:   f,
		sem: make(chan struct{}, 1),
	}

	return func(ctx context.Context) (T, error) {
		if d.done.Load() {
			return d.value, nil
		}

		select {
		case d.sem <- struct{}{}:
			defer func() { <-d.sem }()
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}

		// Check again after acquiring the semaphore.
		if d.done.Load() {
			return d.value, nil
		}

		value, err := d.f(ctx)
		if err != nil {
			var zero T
			return zero, err
		}

		d.value = value
		d.done.Store(true)

		d.f = nil // Allow f to be garbage collected.

		return d.value, nil
	}
}
