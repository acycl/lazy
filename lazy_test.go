package lazy

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
)

func TestFunc_ExecutesOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32

		f := Func(func(ctx context.Context) (int, error) {
			calls.Add(1)
			return 42, nil
		})

		result, err := f(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != 42 {
			t.Fatalf("got %d, want 42", result)
		}

		// Call again - should return cached value.
		result, err = f(context.Background())
		if err != nil {
			t.Fatalf("unexpected error on second call: %v", err)
		}
		if result != 42 {
			t.Fatalf("got %d, want 42", result)
		}

		if got := calls.Load(); got != 1 {
			t.Fatalf("function called %d times, want 1", got)
		}
	})
}

func TestFunc_ConcurrentCalls(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		started := make(chan struct{})
		proceed := make(chan struct{})

		f := Func(func(ctx context.Context) (string, error) {
			calls.Add(1)
			close(started)
			<-proceed
			return "result", nil
		})

		// Holds results from concurrent goroutines.
		type result struct {
			val string
			err error
		}
		results := make(chan result, 3)

		// Launch first goroutine - it will execute f and block.
		go func() {
			v, err := f(context.Background())
			results <- result{v, err}
		}()

		// Wait for first goroutine to start executing f.
		<-started

		// Launch two more goroutines - they should block on the semaphore.
		go func() {
			v, err := f(context.Background())
			results <- result{v, err}
		}()
		go func() {
			v, err := f(context.Background())
			results <- result{v, err}
		}()

		// Let all goroutines reach their blocking points.
		synctest.Wait()

		// Allow f to complete.
		close(proceed)

		// Collect all results.
		for range 3 {
			r := <-results
			if r.err != nil {
				t.Errorf("unexpected error: %v", r.err)
			}
			if r.val != "result" {
				t.Errorf("got %q, want %q", r.val, "result")
			}
		}

		if got := calls.Load(); got != 1 {
			t.Errorf("function called %d times, want 1", got)
		}
	})
}

func TestFunc_ErrorAllowsRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		errTemporary := errors.New("temporary failure")

		f := Func(func(ctx context.Context) (int, error) {
			n := calls.Add(1)
			if n == 1 {
				return 0, errTemporary
			}
			return 100, nil
		})

		// First call fails.
		_, err := f(context.Background())
		if !errors.Is(err, errTemporary) {
			t.Fatalf("got error %v, want %v", err, errTemporary)
		}

		// Second call should retry and succeed.
		result, err := f(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != 100 {
			t.Fatalf("got %d, want 100", result)
		}

		// Third call should return cached success.
		result, err = f(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != 100 {
			t.Fatalf("got %d, want 100", result)
		}

		if got := calls.Load(); got != 2 {
			t.Fatalf("function called %d times, want 2", got)
		}
	})
}

func TestFunc_ContextCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := make(chan struct{})
		proceed := make(chan struct{})

		f := Func(func(ctx context.Context) (int, error) {
			close(started)
			<-proceed
			return 1, nil
		})

		done := make(chan struct{})

		// First goroutine acquires the semaphore and blocks.
		go func() {
			f(context.Background())
			close(done)
		}()
		<-started

		// Second goroutine waits with a cancellable context.
		ctx, cancel := context.WithCancel(context.Background())
		resultCh := make(chan error, 1)
		go func() {
			_, err := f(ctx)
			resultCh <- err
		}()

		// Wait for second goroutine to block on semaphore.
		synctest.Wait()

		// Cancel the waiting goroutine.
		cancel()

		// The cancelled goroutine should return context.Canceled.
		err := <-resultCh
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got error %v, want %v", err, context.Canceled)
		}

		// Let the first goroutine complete.
		close(proceed)
		<-done
	})
}

func TestFunc_PropagatesContextToFunction(t *testing.T) {
	type ctxKey struct{}

	f := Func(func(ctx context.Context) (string, error) {
		v, ok := ctx.Value(ctxKey{}).(string)
		if !ok {
			return "", errors.New("context value not found")
		}
		return v, nil
	})

	ctx := context.WithValue(context.Background(), ctxKey{}, "hello")
	result, err := f(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Fatalf("got %q, want %q", result, "hello")
	}
}

func TestFunc_ZeroValueOnError(t *testing.T) {
	f := Func(func(ctx context.Context) (int, error) {
		return 999, errors.New("fail")
	})

	result, err := f(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if result != 0 {
		t.Fatalf("got %d, want zero value 0", result)
	}
}

func TestFunc_PointerType(t *testing.T) {
	type data struct{ value int }

	f := Func(func(ctx context.Context) (*data, error) {
		return &data{value: 42}, nil
	})

	r1, _ := f(context.Background())
	r2, _ := f(context.Background())

	if r1 != r2 {
		t.Fatal("expected same pointer on subsequent calls")
	}
	if r1.value != 42 {
		t.Fatalf("got %d, want 42", r1.value)
	}
}

func TestFunc_SemaphoreReleasedAfterSuccess(t *testing.T) {
	// This test verifies that the semaphore is properly released,
	// which was a bug when d.sem was set to nil before the defer ran.
	synctest.Test(t, func(t *testing.T) {
		proceed := make(chan struct{})

		f := Func(func(ctx context.Context) (int, error) {
			<-proceed
			return 1, nil
		})

		done := make(chan int, 2)

		// First caller acquires semaphore and blocks.
		go func() {
			v, _ := f(context.Background())
			done <- v
		}()
		synctest.Wait()

		// Second caller blocks on semaphore.
		go func() {
			v, _ := f(context.Background())
			done <- v
		}()
		synctest.Wait()

		// Release the first caller.
		close(proceed)

		// Both should complete without deadlock.
		r1 := <-done
		r2 := <-done

		if r1 != 1 || r2 != 1 {
			t.Fatalf("got results %d and %d, want 1 and 1", r1, r2)
		}
	})
}

func TestFunc_SemaphoreReleasedAfterError(t *testing.T) {
	// Verifies semaphore is released even when f returns an error.
	synctest.Test(t, func(t *testing.T) {
		var attempt atomic.Int32

		f := Func(func(ctx context.Context) (int, error) {
			if attempt.Add(1) == 1 {
				return 0, errors.New("first attempt fails")
			}
			return 42, nil
		})

		// First call fails.
		_, err := f(context.Background())
		if err == nil {
			t.Fatal("expected error on first call")
		}

		// Second call should be able to acquire semaphore and succeed.
		result, err := f(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != 42 {
			t.Fatalf("got %d, want 42", result)
		}
	})
}
