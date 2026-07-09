# Bundle or prebuild the dictionary vs the 67 MB runtime `dict update`

**Status**: P? — needs a spike
**Effort**: M
**Related**: `TODO.md` P? · `cmd/dictupdate.go` · `internal/engine/dictdata.go` · `internal/engine/localdict.go` · [release-binaries.md](release-binaries.md)

## Context

2026-07. The offline bilingual dictionary (CC-CEDICT zh→en + ECDICT en→zh) is
**not** shipped with the binary. First use requires `translate dict update all`,
a one-time ~67 MB download + ECDICT sqlite build (the build "takes a minute").
Until then, Chinese lookups prompt to run the update and English lookups fall
back to dictionaryapi.dev. This is friction on a fresh machine, and it's
per-machine (data lands in `~/.local/share/translate/dict`).

## Investigation

- Acquisition today: `cmd/dictupdate.go` → `engine.DownloadCedict` (gz text) and
  `engine.BuildEcdictDB` (downloads ECDICT, builds a local sqlite via
  `modernc.org/sqlite`).
- The code comment marks explicit `dict update` as "the blessed path"
  (`AutoDownload: false`), deliberately not auto-downloading.
- Licensing: CC-CEDICT is **CC BY-SA 4.0** (© MDBG / Paul Denisowski); ECDICT is
  **MIT** (© skywind3000). Redistribution is allowed with attribution — so
  bundling/shipping is legally fine if attribution is preserved (already in
  README).

## Options considered

| Option | Pros | Cons |
|---|---|---|
| A. Runtime `dict update` (current) | tiny binary; user opts in; always fresh | 67 MB first-run download + minute-long build; per-machine |
| B. `go:embed` a trimmed/compressed DB | offline out of the box; one binary | binary balloons (tens of MB); rebuild to refresh data; embeds a snapshot |
| C. Ship prebuilt DB as a release asset | no per-host build; smaller than embed-all | needs the release pipeline ([release-binaries.md](release-binaries.md)); still a download |
| D. Prebuild sqlite in CI, `go:embed` only that | skips the slow local build step | still big; still a snapshot |

## Current blocker / open questions

- How big is a *trimmed* DB (common headwords only) vs full? Full ECDICT sqlite is
  the heavy part — measure before deciding embed vs asset.
- Is offline-on-first-run actually wanted, or is the API fallback + opt-in update
  the right default? (The author deliberately chose opt-in.)
- Option C couples to the release-binaries spike; do that first.

## Decision (if any)

`2026-07 deferred` — keep opt-in `dict update`. Reconsider embedding/shipping a
prebuilt DB if first-run friction becomes a real complaint, and prefer measuring
a trimmed-DB size before committing to `go:embed`.

## References

- CC-CEDICT: https://www.mdbg.net/chinese/dictionary?page=cedict
- ECDICT: https://github.com/skywind3000/ECDICT
- `go:embed`: https://pkg.go.dev/embed
