I am about to open and review a specific document related to your project.

Found "**/*.go"

Searched "func Main\b"

Allow running this command?

Allow running this command?

**Code Review — `sxcli-fw`**

**Scope & verification**
Reviewed all 24 production files and sampled the test suite (root package + `internal/{config,fail,graph,logging,registry}`), cross-checked against `docs/superpowers/specs/2026-07-09-sxcli-fw-design.md` and the `CLAUDE.md` conventions.

Verification results — all clean:
- `go build ./...`, `go vet ./...` — OK
- `go test ./...` and `go test -race ./...` — all packages pass
- `GOOS=windows go build ./...` — OK (windows files compile)
- `gofmt -l` — nothing to reformat

**Overall assessment**
This is an unusually well-crafted codebase for its stage: exemplary doc comments, a written design spec the code actually follows, deliberate error-collection semantics ("report every startup problem at once"), security-conscious config handling, and a strong, hermetic test suite (~1,800 test lines vs ~1,600 production lines). The project conventions (`z_`/`x_` test prefixes, nested-if style) are applied consistently. The framework core (`Main()`, dispatch, lifecycle, `Tr()`, sinks, YAML provider) is not yet implemented — findings below treat that as roadmap, not defect.

---

**🔴 High — real defect (spec violation)**

**1. `pinned_open_windows.go:18` — junctions are no longer rejected on Go ≥ 1.23.**
The doc comment (and the spec, §6 "Security") promises refusing "reparse points (symlinks, junctions)", but the code checks only `fi.Mode()&os.ModeSymlink == 0`. Since Go 1.23, `os.Lstat` reports mount points/junctions as `ModeIrregular`, **not** `ModeSymlink` — so a junction next to the real binary passes the check and gets opened. Given this file exists precisely to defeat attacker-controlled redirection, the check should be positive rather than negative:

```go
if fi.Mode().IsRegular() {
    // open
} else {
    // reject: not a regular file (symlink, junction, device, ...)
}
```

This also matches the stated intent "must be a regular file" and future-proofs against other special file types.

---

**🟡 Medium — robustness / hardening**

**2. `internal/config/file.go:23–36` — a provider claiming extension `"json"` hijacks native JSON handling.** `byExt` is built from providers without excluding `json`; in both the explicit-path and search branches `byExt["json"]` would be non-nil and the file gets transcoded through the provider instead of parsed natively. Nothing validates this at registration. Suggest: treat a provider claiming `json` as a startup violation (or explicitly skip it).

**3. `internal/config/file.go:96–114` — trailing garbage after the JSON object is silently accepted.** `dec.Decode(&section)` reads the first value only; `{"a":{}} {"b":{}}` or `{...}junk` parses fine and drops the rest. Add the usual second-decode/EOF check so malformed files are loud (matches the project's "loud startup error" philosophy).

**4. `internal/config/file.go:27` — duplicate extension claims are silently first-wins.** If two providers both claim `yaml`, the second is ignored without a violation. Everywhere else conflicts (duplicate args, duplicate env names, ambiguous candidate files) are reported — this one should be too.

**5. `internal/graph/graph.go:233` — `order()` silently degrades on a broken invariant.** `pos[target.ID]` returns `0` on a map miss, which would fabricate an edge to (or a self-loop on) member 0. It's currently safe — every binding target provably lands in the closure — but the safety rests on a subtle consistency argument between `expandDep` and `bindDep` (both picking `candidates(dep)[0]`). A `, ok` check with an `internal:` failure (like the existing `"condensation is not a DAG"` guard) would make the invariant explicit and future-proof.

**6. Optional-by-id semantics are asymmetric (`internal/graph/graph.go:113–129`).** For `inject:"someid;optional"`: a *disabled* target is tolerated, but an *unregistered* id is a hard error even though the field is optional. That's defensible (typo protection), but it's undocumented — the public docs only say "zero matches leaves a nil field". Worth documenting the intent on the `inject` grammar, or aligning behavior.

**7. Error-gating invariant around `JSONPath` (`internal/config/file.go:143`, `fill.go:173`).** Fields whose `json` tag is missing keep a nil `JSONPath` yet are still appended to the schema; `applyObject` and `MarshalIndent` index `f.JSONPath[depth]` unconditionally. Today registration-time `ValidateConfig` + collector gating prevents reaching that code, but the panic-if-ungated invariant is invisible at the panic site. A short comment (or skipping tag-invalid fields during `extract`) would protect future refactors.

**8. `internal/config/file.go:77` — misleading error message.** The `open != nil` guard covers plain `src.Open` being nil too, but the message always says "pinned location without a pinned opener".

---

**🟢 Low — polish and consistency**

- **`go.mod:5`** — `golang.org/x/sys` is marked `// indirect` but is directly imported by `types_windows.go`; `go mod tidy` will fix the marker.
- **`types_windows.go:19`** — unfinished doc: "TODO: document the exact state once the implementation is finished."
- **`suppress.go:46–54`** — repeated `Suppress` calls append duplicate long names (harmless but untidy); also `Help` is deliberately(?) not suppressible while every other core surface is — worth a comment. Note the derived env name means `MYAPPLET_HELP=true` can trigger help mode purely from the environment.
- **Last-error-wins loops** — `parseInjectTag` (`registry.go:157–181`), slice element loops in `setFromJSON`/`setSliceFromEnv` (`fill.go`): a later error overwrites an earlier one, so only one violation per field/tag is reported. Minor divergence from the "report all problems at once" goal.
- **`registry.go:185` — `isValidID` accepts `"_"`** (the blank identifier) as a service id.
- **`schema.go:174`** — `sf.Type != durationType` is dead: `time.Duration`'s kind is `Int64`, never `Struct`. Either remove it or (probably the real intent) use it to reject field-less struct types like `time.Time`, which currently become silent, empty file-only sections instead of an "unsupported type" error.
- **`fill.go:148–162`** — `--write-config` emits `null` for nil slices instead of `[]` (round-trips fine, but ugly in generated files); env values cannot express an empty slice (`FOO=` yields one empty element → error for non-string elements).
- **`pinned_open.go:22`** — the friendly "is a symlink" message triggers on `ELOOP` only; some BSDs report `EMLINK`/`EFTYPE` for `O_NOFOLLOW`. Open still fails safely there, just without the nice message.
- **`multi.go:16–22`** — `Enabled` polls every child even after one accepts; a `break` (or early `true`) saves work on the hottest path in logging. Similarly `Handle` clones per child — conservative and correct, but the last delivery could reuse the record if you ever care about allocations.
- **`buffer.go`** — by design the buffer never drains: a second `Replay` re-delivers everything, and captures continue after replay. Fine for the intended bootstrap lifetime, but a one-line doc note ("replay once, then discard the buffer") would prevent misuse. Unbounded growth is acceptable for startup capture.
- **Doc/code mismatch in `args.go:94`** — `bundle()` computes `last := i == len(shorts)-1` mixing a byte index with byte length; it's only correct because non-ASCII shorts can never be "known". Currently safe, deserves a comment at most.

**Modernization (Go 1.26 idioms)**
Purely optional, low-value-but-easy sweeps:
- `reflect.TypeOf((*I)(nil)).Elem()` → `reflect.TypeFor[I]()` (`register.go:35`, `schema.go:14`, tests).
- Counted loops → `for i := range n` (`graph.go`, `schema.go:121`, `fill.go:154`, tests).
- Manual contains/any-loops → `slices.Contains` (`matches()` in `graph.go:166–176`, provider-dedup in `file.go:103–106`).
- `if low[w] < low[v] { ... }` → `low[v] = min(low[v], low[w])` (`tarjan`).
- In tests: `t.Context()` instead of `context.Background()`, `wg.Go(...)` instead of `wg.Add(1)`+`go func`.

**Strengths worth keeping**
- **Layering discipline**: `internal/*` packages are framework-ignorant; semantics injected as `registry.Check` hooks; `config.Provider` as a structural twin avoids import cycles; all external inputs flow through `Sources` for hermetic tests.
- **Correct `slog` contracts**: `Buffer` clones records on capture *and* replay, copies op chains so views never alias (`extended()`), and replays `WithAttrs`/`WithGroup` chains faithfully; `Multi` derives child views properly and joins errors without blocking siblings. The race detector confirms the locking.
- **Careful reflection**: typed-nil detection at registration, `FieldByIndexErr` guarding nil embedded pointers in `Inject`, optional slices left nil rather than empty.
- **Deterministic resolution**: Tarjan + condensation with registration-order tie-breaking gives reproducible start order and honest cycle reporting; the test suite pins all of it down (diamonds, self-loops, override steering, late slice joiners).
- **Security thinking**: real-binary-dir pinning, atomic `O_NOFOLLOW`, loud-failure philosophy — the spec §6 rationale is excellent.

**Missing pieces (expected — per spec §2/§5, not yet started)**
`Main()` with dispatch and the 14-step pipeline, the lifecycle driver, `Tr()`, `sink/{console,file,syslog}`, `configfmt/yaml`, `--help`/`--write-config` wiring, the platform layer (`platform_unix.go`/`platform_windows.go`), and integration (`x_*_test.go`) tests — none exist yet. The implemented foundation (registry, graph, config, logging, fail) is in very good shape to build them on.

**Suggested priority**
1. Fix the Windows junction check (`fi.Mode().IsRegular()`) — real security gap vs. spec.
2. Forbid/skip provider claims on `"json"` + report duplicate extension claims.
3. Add the JSON trailing-data check in `parse`.
4. Add the defensive `pos` lookup check in `order()` and document the optional-by-id and `JSONPath` invariants.
5. Everything else is polish; the modernization sweep can ride along with any future change.