// Historically this package registered wasilibs' accelerated matchers
// (go-re2, aho-corasick, libinjection) under TinyGo. That path is retired:
// go-re2 dropped TinyGo support in v1.12.0, nottinygc is archived, and the
// prebuilt C archives no longer link against TinyGo >= 0.38's wasi-libc.
// Coraza's pure-Go operators are used instead; since coraza v3.5/3.6 regex
// memoization and literal pre-filtering are enabled by default, which closes
// most of the performance gap that motivated wasilibs.

package operators

func Register() {}
