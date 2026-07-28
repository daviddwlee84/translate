# translate — dev tasks (run `just` to list)

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
install DIR="~/.local/bin": build
    mkdir -p {{DIR}}
    install -m 0755 translate {{DIR}}/translate

# remove build artifacts
clean:
    rm -f translate

# make the Raycast script-commands executable + show how to add them
raycast-scripts:
    chmod +x raycast/script-commands/*.sh
    @echo "Add in Raycast → Settings → Extensions → Script Commands → Add Script Directory:"
    @echo "  {{justfile_directory()}}/raycast/script-commands"

# run the TS extension in dev (registers it in Raycast; persists after you stop)
raycast-dev:
    cd raycast/extension && ([ -d node_modules ] || npm install) && npm run dev

# type-check / build the extension bundle (does NOT install into Raycast)
# NOTE: `ray build` defaults to -e dev, where esbuild STRIPS types without
# checking them — a genuine type error still prints "built successfully".
# Use `raycast-check` before trusting a build.
raycast-build:
    cd raycast/extension && ([ -d node_modules ] || npm install) && npm run build

# lint the extension with the Raycast eslint config
raycast-lint:
    cd raycast/extension && ([ -d node_modules ] || npm install) && CI=true npm run lint

# assert the pure modules (no test runner in a Raycast extension — see
# src/lib/dev-check.ts for why, and for the import discipline it depends on)
raycast-verify:
    cd raycast/extension && ([ -d node_modules ] || npm install) \
      && npx tsc --outDir .build/verify --module commonjs --target ES2022 \
           --lib ES2023 --esModuleInterop --strict src/lib/dev-check.ts \
      && node .build/verify/dev-check.js

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
raycast-check: raycast-verify
    cd raycast/extension && npx ray build
    cd raycast/extension && npx tsc --noEmit -p tsconfig.json
    cd raycast/extension && CI=true npx ray lint
    cd raycast/extension && npx ray build -e dist

# print the JSON schemas Raycast AI will see for each tool in src/tools/.
# Read it — a field with no "description", or empty "properties", ships a tool
# the model has to guess at, and neither lint nor build complains.
raycast-tool-schemas:
    cd raycast/extension && npx ray build --print-tool-schemas

