# go install fails: "module declares its path as: translate" but required as github.com/...

**Symptoms** (grep this section): `go install` error `module declares its
path as: translate`, `but was required as: github.com/daviddwlee84/translate`,
`parsing go.mod`; `go install github.com/...@latest` refuses to install a repo
whose `go.mod` says `module translate`.
**First seen**: 2026-07
**Affects**: any Go module whose `module` path in `go.mod` doesn't match its
import/repo URL; `go install <path>@<ver>`; Go 1.16+ (module-aware install).
**Status**: fixed — module renamed to the GitHub path.

## Symptom

```
$ go install github.com/daviddwlee84/translate@latest
go: github.com/daviddwlee84/translate@v0.1.0: parsing go.mod:
	module declares its path as: translate
	        but was required as: github.com/daviddwlee84/translate
```

The project built fine locally (`go build .`) with `module translate` in
`go.mod` and imports like `"translate/internal/config"`. It only breaks when you
try to `go install` it by its resolvable URL, because the declared module path
and the requested path disagree.

## Root cause

`go install <pkg>@<version>` fetches the module and requires that the path in the
downloaded `go.mod` **exactly equals** the path you asked for. A local-only module
name (`module translate`) is legal for `go build` in the working tree but is not a
resolvable import path — `go install github.com/daviddwlee84/translate@latest`
downloads the repo, reads `module translate`, and rejects the mismatch. This is Go
enforcing that a module's identity is its import path.

## Workaround

Rename the module to the repo URL and rewrite every internal import prefix in the
same pass, then rebuild:

```sh
go mod edit -module github.com/daviddwlee84/translate
# rewrite internal imports "translate/..." -> "github.com/daviddwlee84/translate/..."
grep -rl '"translate/' --include='*.go' . \
  | xargs sed -i '' 's#"translate/#"github.com/daviddwlee84/translate/#g'   # macOS BSD sed: -i ''
go build ./... && go vet ./...
```

Guard: only string literals `"translate/…"` are import paths here; bare
`"translate"` occurrences were non-import string literals (a config subdir const
and a keybinding help string), so the prefixed replace was safe. Verify with
`grep -rn '"translate/' cmd internal main.go` returning nothing.

## Prevention

- When creating a Go project you might ever `go install`/publish, name the module
  after its future repo URL from day one: `go mod init github.com/<you>/<repo>`.
- Binary name from `go install` is the **last path element** of the main package's
  module path — keeping the repo named `translate` keeps the binary `translate`.

## Related

- Sibling: [gobin-points-at-mise-toolchain-dir.md](gobin-points-at-mise-toolchain-dir.md)
- Shipped fix: `TODO.md` Done "Publish as a public repo + `go install`"
- Go docs: https://go.dev/ref/mod#go-install
