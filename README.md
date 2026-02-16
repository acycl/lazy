# lazy

[![Go Reference](https://pkg.go.dev/badge/github.com/acycl/lazy.svg)](https://pkg.go.dev/github.com/acycl/lazy)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

Context-aware lazy initialization.

## Overview

Package `lazy` provides a generic, context-aware lazy initialization primitive
for Go. It wraps a function so that it executes at most once successfully,
caching the result for all subsequent callers.

Key properties:

- **Generic** — works with any type via Go generics.
- **Context-aware** — passes the caller's context through to the initializer
  and supports cancellation while waiting.
- **Retries on error** — only a successful result is cached; errors allow
  future callers to retry.
- **Concurrency-safe** — multiple goroutines can call the returned function
  simultaneously; only one executes the initializer at a time.

## Install

Requires Go 1.25 or later.

```sh
go get github.com/acycl/lazy
```

## Usage

### Basic initialization

```go
getConfig := lazy.Func(func(ctx context.Context) (*Config, error) {
    return loadConfigFromDisk(ctx)
})

// First call executes the function.
cfg, err := getConfig(ctx)

// Second call returns the cached result.
cfg, err = getConfig(ctx)
```

### Automatic retry on error

If the initializer returns an error, the result is not cached and the next
caller will retry.

```go
connect := lazy.Func(func(ctx context.Context) (*sql.DB, error) {
    return sql.Open("postgres", connStr)
})

db, err := connect(ctx) // fails — transient network error
db, err = connect(ctx)  // retries the initializer
```

### Context cancellation

Callers waiting for an in-flight initialization can bail out early by
cancelling their context. This does not affect the goroutine that is actively
running the initializer.

```go
getToken := lazy.Func(func(ctx context.Context) (string, error) {
    return fetchToken(ctx)
})

ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
defer cancel()

token, err := getToken(ctx) // returns context.DeadlineExceeded if the timeout fires
```

## License

[Apache-2.0](LICENSE)
