# sxcli-fw — project conventions

## Test file naming

- Unit test sources must be named `z_*_test.go` (e.g. `z_lifecycle_test.go`).
- Integration test sources must be named `x_*_test.go` (e.g. `x_applet_test.go`).

(The `_test.go` suffix is required by the Go toolchain; the `z_`/`x_` prefix
distinguishes unit from integration tests.)

## Code style

- Nested `if` statements are mandatory — do not flatten them into guard
  clauses / early returns.
- Complex boolean expressions are allowed.
