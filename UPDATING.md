# Updating dependencies

This connector compiles Coraza + OWASP CRS to WebAssembly with TinyGo. The
pieces are tightly coupled, so bump them together and always verify with the
full test loop below.

## Version coupling (verified July 2026)

| Component | Pinned at | Coupling |
|---|---|---|
| coraza/v3 | v3.7.0 | v3.4.0+ requires Go 1.25 → TinyGo ≥ 0.39 |
| coraza-coreruleset/v4 | v4.25.0 | ≥ v4.24.1 requires coraza ≥ v3.4.0 (`SecRequestBodyJsonDepthLimit` in `@coraza.conf-recommended`) |
| Go | 1.25.x | driven by coraza's go.mod |
| TinyGo | **0.39.x (pinned)** | 0.40/0.41 guests crash under wazero at instantiation (OOB in `runtime.initAll`) — re-test before bumping |
| go-ftw (testing module) | v2.x | v0.6.x panics on current CRS test yamls |

Dependabot (`.github/dependabot.yml`) raises weekly PRs for the Go modules in
`/` and `/testing/coreruleset`; CI runs the full loop on each PR. TinyGo and Go
versions are pinned in `.github/workflows/ci.yaml` and
`nightly-coraza-check.yaml` and must be bumped manually.

## Notable build specifics

- **No wasilibs / nottinygc.** The prebuilt C archives (go-re2, aho-corasick,
  libinjection) and the custom GC are gone: go-re2 retired TinyGo support in
  v1.12.0 and nottinygc is archived. The build uses coraza's pure-Go matchers
  (regex memoization and literal pre-filtering are on by default since coraza
  v3.5/3.6).
- **`-gc=boehm` is required, not optional.** TinyGo's default (precise) GC
  showed 30s+ stop-the-world pauses on this workload's ~65 MB heap — single
  requests would stall for seconds in production. With Boehm the full FTW
  suite (13.7k requests) completes in under a minute even on a 2-CPU VM.
  Note Boehm needs the 8 MB stack too: with the default 64 KB stack it
  crashes at `GC_init` during instantiation.
- **`wasip1-8mb-stack.json`** raises the wasm main stack from wasm-ld's 64 KB
  default to 8 MB — loading the full CRS at startup compiles regexes
  recursively and overflows a 64 KB stack. The CLI `-stack-size` flag does NOT
  affect the main stack; only the `-z stack-size` linker flag does.
- **`-buildmode=wasi-legacy`** keeps the module callable after `main()`
  returns (TinyGo ≥ 0.35 otherwise calls `proc_exit`).
- **`internal/e2e`** is coraza v3.3.3's e2e suite, vendored: newer suites add
  an SSE-streaming case that a response-buffering connector (http-wasm
  requires `FeatureBufferResponse`) can never pass.
- Rules without an explicit `severity` action are `RuleSeverityUnset` since
  coraza v3.4 — `errorCb` in `main.go` must keep its `default:` branch or
  go-ftw's log-marker rule stops being logged and every FTW test fails with
  "can't find log marker".

## Verification loop

```sh
docker run --rm -v "$PWD:/src" -w /src -e GOFLAGS=-buildvcs=false \
  tinygo/tinygo:0.39.0 sh -c '
    go run mage.go build &&
    go test ./... &&
    go test -count=1 -run "^TestE2E" -tags e2e . &&
    go run mage.go ftw'
```

The FTW suite (~4k CRS regression tests through wazero) is the real gate —
"builds" alone proves very little in this repo. With `-gc=boehm` it completes
in about a minute; most wall time is compiling the wasm and the Go test deps.
`FTW_INCLUDE='^942150' go test ./testing/coreruleset` runs a subset when
debugging individual rules. When bumping CRS, expect a handful of new test
failures — classify each (response-body class, Go-vs-Apache platform
difference, known Coraza issue) and extend `.ftw.yml` with a documented
reason; cross-check coraza-proxy-wasm's `ftw/ftw.yml` for upstream-known
skips.
