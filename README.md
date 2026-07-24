# coraza-http-wasm

Web Application Firewall WASM middleware built on top of [Coraza](https://coraza.io/) and implementing the [http-wasm](https://http-wasm.io/) ABI.

Sharp Point Data's fork of [jcchavezs/coraza-http-wasm](https://github.com/jcchavezs/coraza-http-wasm), modernized off upstream v0.3.0 (October 2024) and packaged as a Traefik middleware plugin.

## Current state

| | |
|---|---|
| Coraza | v3.7.0 |
| OWASP CRS | v4.25.0 (embedded in the binary) |
| Go / TinyGo | 1.25 / 0.39.0 (both pinned) |
| Build output | `build/coraza-http-wasm.wasm`, ~3.5 MB |
| Test gate | ~4.6k CRS regression tests (go-ftw) on every push and PR |

What diverges from upstream v0.3.0:

- Coraza v3.2.1 → v3.7.0 and CRS v4.0.0 → v4.25.0. The CRS jump is 25 minor
  releases of rule tightening, so expect a different false-positive profile.
- The wasilibs matchers and `nottinygc` are gone, replaced by Coraza's pure-Go
  operators. go-re2 retired TinyGo support in v1.12.0, nottinygc is archived,
  and their prebuilt C archives no longer link against TinyGo ≥ 0.38.
- Built with `-gc=boehm` on a custom 8 MB-stack wasip1 target
  (`wasip1-8mb-stack.json`) and `-buildmode=wasi-legacy`.
- Ships a `.traefik.yml` manifest and a self-contained release bundle. Upstream
  keeps those in a separate `coraza-http-wasm-traefik` repo; here they live
  alongside the source.

**The build flags are load-bearing, not preferences** — the default GC stalls
for 30s+ on this workload's ~65 MB heap, and a 64 KB stack overflows while
compiling CRS regexes at startup. [UPDATING.md](UPDATING.md) documents the
version coupling, each flag's reason, and the bump playbook. Read it before
changing a dependency or a toolchain pin.

## Getting started

`go run mage.go -l` lists all the available commands:

```bash
$ go run mage.go -l
Targets:
  build*    builds the wasm binary.
  e2e       runs e2e tests
  format    formats code in this repository.
  ftw       runs the FTW test suite
  lint      verifies code format.
  test      runs all unit tests.

* default target
```

### Building the binary

```bash
go run mage.go build
```

You will find the WASM plugin under `./build/coraza-http-wasm.wasm`.

Note that `build` alone proves very little here: it is the FTW suite
(`go run mage.go ftw`) that exercises the ruleset through wazero the way a
proxy will. UPDATING.md has a containerized one-liner that runs the whole loop.

### Basic Configuration

```json
{
   "directives": [
    "SecRuleEngine On",
    "SecDebugLog /dev/stdout",
    "SecDebugLogLevel 9",
    "SecRule REQUEST_URI \"@streq /admin\" \"id:101,phase:1,log,deny,status:403\""
   ]
  }
```

`directives` is the only configuration key. Values are joined with newlines and
handed to Coraza as seclang, so anything valid in a `.conf` file works,
including `Include @coraza.conf-recommended`, `Include @crs-setup.conf.example`
and `Include @owasp_crs/*.conf` to load the embedded ruleset.

### Test it

```console
curl -I 'http://localhost:8080/admin'    # 403
curl -I 'http://localhost:8080/anything' # 200
```

## Using it as a Traefik plugin

Pushing a tag runs the full test suite and then attaches two assets to the
release for that tag:

- `coraza-http-wasm-<tag>.zip` — a flat bundle of `.traefik.yml`,
  `coraza-http-wasm.wasm` and `LICENSE`.
- `coraza-http-wasm-<tag>-checksums.txt` — sha256 of the bundle and of the bare
  wasm.

The workflow creates the release as a **draft**. Draft assets are only
downloadable by accounts with push access, so publish the release before any
unauthenticated consumer tries to fetch it.

Load it as a local plugin rather than through the public Traefik plugin
catalog. Unpack the bundle so the manifest and the binary sit together under
Traefik's plugin directory:

```
/plugins-local/src/github.com/Sharp-Point-Data/coraza-http-wasm/
├── .traefik.yml
└── coraza-http-wasm.wasm
```

```yaml
experimental:
  localPlugins:
    coraza:
      moduleName: github.com/Sharp-Point-Data/coraza-http-wasm
```

For `localPlugins` the `moduleName` is just that directory path — it is not
resolved as a Go module, so it does not need to match `go.mod`. On Kubernetes,
delivering the bundle as an image copied in by an init container keeps the
plugin out of Traefik's startup network path: catalog-hosted plugins are
re-downloaded on every pod start, and a fetch failure breaks every router that
references the middleware.

Then reference the plugin from a middleware, with `directives` as above:

```yaml
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: waf
spec:
  plugin:
    coraza:
      directives:
        - Include @coraza.conf-recommended
        - SecRuleEngine On
        - Include @crs-setup.conf.example
        - Include @owasp_crs/*.conf
```

The plugin key (`coraza`) must match between the static configuration and
`spec.plugin`. A directive the parser rejects — a bad include name, or a PCRE
construct such as a lookahead, since Coraza uses RE2 — fails the middleware
build and takes down every router referencing it, so validate changes on a
non-production router first.
