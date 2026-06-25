# Contributing

Thanks for helping improve `uptime`.

This project is intentionally small: a `net/http` uptime history middleware, shared storage backends, a built-in status page, and a JSON status API. Please keep changes focused on that scope.

## Development Requirements

- Go 1.22 or newer

Before opening a pull request, run:

```sh
test -z "$(gofmt -l .)"
go test ./...
go vet ./...
```

For concurrency-sensitive changes, store implementations, alert state, or probe behavior, also run:

```sh
go test -race ./...
```

PostgreSQL integration tests are optional and require:

```sh
UPTIME_POSTGRES_DSN='postgres://user:password@host:5432/postgres?sslmode=disable' go test ./store/postgres -v
```

## Design Guidelines

- Keep the public API small and easy to use.
- Prefer the Go standard library unless a dependency has clear value.
- Keep authentication and access control outside this package.
- Keep alert delivery optional and user-owned through hooks.
- Keep external probing in the optional `probe` package, not the core middleware.
- Do not read request bodies or capture response bodies by default.
- Do not add frontend build tooling or external browser assets.
- Keep request-path overhead low.
- Document concurrency behavior for reusable exported types.
- Avoid high-cardinality metrics or request-route analytics in this package.

## Pull Requests

Please include:

- a short description of the change
- tests for behavior changes
- documentation updates when public behavior or configuration changes

Avoid mixing unrelated refactors with feature or bug-fix changes.
