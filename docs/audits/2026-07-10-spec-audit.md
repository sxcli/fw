# sxcli-fw spec ↔ implementation audit — 2026-07-10

Auditor: Fable 5 agent, maximum effort, exhaustive claim-by-claim
verification (118 normative claims extracted from the spec, each
verified against code, not names or comments).

**Spec:** `docs/superpowers/specs/2026-07-09-sxcli-fw-design.md`
(650 lines). Claims by area: interfaces/signatures 12, enforced rules 5,
registration 14, DI 10, dispatch 7, pipeline & short-circuits 17,
service control 4, failure semantics 5, config tags/types 8,
sources/precedence/security 14, suppression/caps 5, format providers 5,
argument syntax 7, logging 11, Tr 4, Windows/SCM 8, module layout &
misc 6, testing strategy 4.

**Sanity run:** `go build ./...` OK, `go vet ./...` OK,
`go test -short ./...` all 9 test packages pass (wine tests skipped).

**Counts:** ALIGNED **99** · DIVERGED **9** · UNIMPLEMENTED **0** ·
DEFERRED **10** (all in the Open Items table) · UNDOCUMENTED **13**.

Aligned highlights: all 7 public interfaces match spec signatures
exactly (incl. `SCMApplet` in `types_windows.go`); all 8 registration
violation classes fire without panicking; DI matching/optional/override/
disable semantics, SCC ordering with registration-order ties,
ejection-before-Configured, exact lifecycle order, exit-code-2 policy,
buffer/replay/swap, console force-pull with deliberate-silence
exception, stderr last-resort, O_NOFOLLOW pinned open (+ Windows
IsRegular Lstat), stat-before-open size cap with capped reader, YAML
provider seeding of used providers, `Tr` pair/escape/verbatim
semantics, `--scm-debug` opt-in gating (wine-tested), env derivation
`APPLETID_` + upper(long).

## DIVERGED (9)

**D1. Run-scoped core fields are settable from a config file — worst
finding.** Spec: `--config` "cannot meaningfully come from a config
file" (§6) and help suppression "closes both doors" — files are not a
door. Code: `Files.ApplyCore` (`internal/config/load.go:24-33`) applies
the file's `core` section to *all* core fields, and `main.go:161-166`
honors them. `{"core":{"help":true}}` turns every run into help output;
`{"core":{"writeConfig":true,"config":"/path"}}` makes every run write
a 0600 file to `/path` and exit 0 without running the applet. The
`dump:"-"` fix covered the write side only. Judgment: **code is
wrong** — a read-side counterpart to `dump:"-"` is missing.

**D2. `--help` prints merged values, not defaults.** Spec §5 says
"defaults"; `main.go:359` prints the current merged value, labeled
`value:`. Judgment: spec wording stale; code is more useful. Spec fix.

**D3. Multi-applet `--help` has no applet list.** Spec §5 rule 5
implies it should have one; `rt.help()` never renders a list — only
dispatch-failure `usage()` does. Judgment: spec sloppy; help runs
post-dispatch where the list adds little.

**D4. Unknown-extension config files in searched locations are
silently ignored.** Spec §6 makes unknown extensions a startup error;
code enforces it only for the explicit `--config` path — the location
search probes only `json` + registered extensions, so a stray
`<applet>-config.toml` is never seen. Judgment: scope the spec sentence
or scan the location's directory.

**D5. `--write-config` with a missing target re-enables the location
search.** Spec: non-empty `--config` skips the search "entirely"; code
(`main.go:276-284`) blanks a missing write-config target so the search
feeds the written file. Judgment: intended behavior; spec should carve
out the case.

**D6. A dangling symlink at the pinned companion is a silent skip.**
Spec: "A symlinked companion is a loud startup error, never a silent
skip." `statRegular` follows symlinks; a dangling one reports
`ErrNotExist` → skipped. Only a symlink to an existing regular file is
caught by `openPinned`. Judgment: `Lstat` cross-check at pinned
locations closes it. Low severity.

**D7. Env-tag-only fields are environment-settable.** Spec: "no [arg]
tag → no CLI arg and no env var (file-only)"; code honors an explicit
`env:"NAME"` without an `arg` tag. Judgment: spec self-contradictory;
code is the coherent reading. Spec fix.

**D8. Mode 0600 not enforced on an existing `--write-config` target.**
`os.WriteFile` mode applies only on create; normalizing an existing
0644 file leaves it 0644. Judgment: minor; chmod or scope the spec.

**D9. Shipped syslog sink cannot honor the prompt-Handle deadline
contract remotely.** stdlib `log/syslog` exposes no deadlines
(acknowledged in `sink/syslog/types.go`, not in the spec). Judgment:
spec should carry the caveat.

## UNIMPLEMENTED (0)

Every non-deferred promise has an implementation. (`internal/platform/`
is conditional in the spec and correctly absent.)

## UNDOCUMENTED (13)

1. `--` end-of-flags separator (`args.go`).
2. Ambiguous-candidate error at one search location (`file.go`).
3. The `dump:"-"` tag, usable by any consumer config struct.
4. Bare tokens must trail — "positionals must come last" strict error.
5. Duplicate explicit env names across the closure = startup error.
6. Single-valued inject fields may name at most one ID; the spec
   grammar allows `id1,id2` unqualified.
7. Lexical rules for long/short/env names; embedded config-struct
   fields rejected.
8. Duration strictness: unit suffix required from args/env; JSON
   durations must be strings, never numbers.
9. Trailing data after the config JSON object = startup error.
10. Graph controls: enable+disable of the same id = error, disabled
    applet = error, override keys may be unregistered names, unused
    overrides silently ignored.
11. Windows specifics pinned in code only: `%ProgramData%` system
    config root, `.exe` stripping for argv[0] dispatch, non-`SCMApplet`
    applet under the SCM → logged error + exit 2.
12. Build-time API misuse is collected as violations:
    `MaxConfigSize(≤0)`, `Suppress(FeatureSCMDebug)`,
    `Enable(default-on feature)`.
13. Slice-arg reset semantics: the first argument occurrence wipes
    file/env-sourced content, repetitions append.

## DEFERRED (10)

All ten Open Items table rows check out as genuinely deferred
(`ConfigurationUpdater` never invoked, `Positionals()` unrouted,
synchronous multihandler, etc.).

**Bottom line:** strong agreement — no unimplemented promises, one real
behavioral hole (D1), the rest spec-wording drift plus security-adjacent
edge cases (D4, D6, D8) needing deliberate decisions.
