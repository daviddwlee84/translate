# translate — dev tasks (run `just` to list)
#
# ONE Justfile, both platforms. `just` picks a recipe by the [unix] / [windows]
# attribute, so the same name can have two bodies and `--list` still shows it
# once. Only the genuinely shell-specific recipes are split; everything else is
# written to run under sh AND pwsh 7 (which does support `&&`), so the
# duplication stays at two recipes rather than ten.
#
# `set windows-shell` is what stops `just` reaching for `sh` on Windows — the
# default, and the reason a POSIX-only recipe fails there with a confusing
# "program not found" rather than a syntax error.
set windows-shell := ["pwsh.exe", "-NoLogo", "-Command"]

# show available recipes
default:
    @just --list

# build the binary into ./translate
build:
    go build -o translate .

# run: `just run "hola mundo" --to en`  ·  `just run` for the TUI
run *ARGS:
    go run . {{ARGS}}

# launch the interactive TUI
tui:
    go run .

# dictionary lookup: `just define ephemeral`
define WORD:
    go run . define {{WORD}}

# guided config wizard
init:
    go run . init

# gofmt, vet, and build
check: fmt vet build

# format the source tree
fmt:
    gofmt -w cmd internal main.go

# static analysis (scoped to the Go tree; skips raycast/extension/node_modules)
vet:
    go vet . ./cmd/... ./internal/...

# tidy go.mod / go.sum
tidy:
    go mod tidy

# run tests
test:
    go test ./cmd/... ./internal/...

# install into ~/.local/bin (first on PATH; override with DIR=…)
# Unix only: Windows gets the binary from `go install` (GOBIN=~\.local\bin),
# which is what the windows-dotfiles chezmoi script does.
[doc("install into ~/.local/bin (first on PATH; override with DIR=…)")]
[unix]
install DIR="~/.local/bin": build
    mkdir -p {{DIR}}
    install -m 0755 translate {{DIR}}/translate

# remove build artifacts
[unix]
clean:
    rm -f translate

[windows]
clean:
    Remove-Item -Force -ErrorAction SilentlyContinue translate, translate.exe

# make the Raycast script-commands executable + show how to add them.
# Unix only — Script Commands are bash, and Raycast for Windows has no
# equivalent surface.
[doc("make the Raycast script-commands executable + show how to add them")]
[unix]
raycast-scripts:
    chmod +x raycast/script-commands/*.sh
    @echo "Add in Raycast → Settings → Extensions → Script Commands → Add Script Directory:"
    @echo "  {{justfile_directory()}}/raycast/script-commands"

# npm install on first use. THE ONLY recipe that needs a platform split in the
# Raycast group: `[ -d … ] || …` is POSIX test syntax with no pwsh equivalent.
# Private (leading _) so it stays out of `just --list`.
[unix]
_raycast-deps:
    @[ -d raycast/extension/node_modules ] || npm --prefix raycast/extension install

[windows]
_raycast-deps:
    @if (-not (Test-Path raycast/extension/node_modules)) { npm --prefix raycast/extension install }

# run the TS extension in dev (registers it in Raycast; persists after you stop)
raycast-dev: _raycast-deps
    cd raycast/extension && npx ray develop

# type-check / build the extension bundle (does NOT install into Raycast)
# NOTE: `ray build` defaults to -e dev, where esbuild STRIPS types without
# checking them — a genuine type error still prints "built successfully".
# Use `raycast-check` before trusting a build.
[doc("build the extension bundle (does NOT typecheck — see raycast-check)")]
raycast-build: _raycast-deps
    cd raycast/extension && npx ray build

# lint the extension with the Raycast eslint config.
# Split only because of the CI=true prefix: `VAR=x cmd` is POSIX, and pwsh
# spells the same thing `$env:VAR='x'`.
[doc("lint the extension with the Raycast eslint config (CI mode)")]
[unix]
raycast-lint: _raycast-deps
    cd raycast/extension && CI=true npx ray lint

[doc("lint the extension with the Raycast eslint config (CI mode)")]
[windows]
raycast-lint: _raycast-deps
    cd raycast/extension && $env:CI='true'; npx ray lint

# assert the pure modules (no test runner in a Raycast extension — see
# src/lib/dev-check.ts for why, and for the import discipline it depends on).
# One long line on purpose: a `\` continuation is POSIX-only (pwsh uses a
# backtick), and `just` runs each line in its own shell anyway.
[doc("assert the pure modules via src/lib/dev-check.ts")]
raycast-verify: _raycast-deps
    cd raycast/extension && npx tsc --outDir .build/verify --module commonjs --target ES2022 --lib ES2023 --esModuleInterop --strict src/lib/dev-check.ts && node .build/verify/dev-check.js

# THE GATE. Each stage catches what the others miss:
#   verify   our own invariants: both shortcut branches, table normalisation
#   build    that esbuild can bundle it, that the tool schemas extract — and it
#            GENERATES raycast-env.d.ts, which is gitignored, so it must run
#            before tsc or a fresh clone has no Preferences/Arguments globals
#   tsc      types (`ray build -e dev` does not typecheck at all)
#   lint     manifest schema, icons, ESLint, Prettier, reserved shortcuts —
#            and, ONLY under CI=true, that package-lock.json points at
#            registry.npmjs.org (a mirror in your global npm config writes a
#            lockfile the Raycast store rejects)
#   dist     the build the store actually produces — this one DOES run tsc
# lint is invoked as a sub-`just` rather than inlined so CI=true is spelled
# per-platform in exactly one place.
[doc("THE GATE: verify → build → tsc → lint → dist")]
raycast-check: raycast-verify
    cd raycast/extension && npx ray build
    cd raycast/extension && npx tsc --noEmit -p tsconfig.json
    just raycast-lint
    cd raycast/extension && npx ray build -e dist

# print the JSON schemas Raycast AI will see for each tool in src/tools/.
# Read it — a field with no "description", or empty "properties", ships a tool
# the model has to guess at, and neither lint nor build complains.
[doc("print the JSON schemas Raycast AI sees for each src/tools/ entry")]
raycast-tool-schemas: _raycast-deps
    cd raycast/extension && npx ray build --print-tool-schemas

