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

# static analysis
vet:
    go vet ./...

# tidy go.mod / go.sum
tidy:
    go mod tidy

# run tests
test:
    go test ./...

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
raycast-build:
    cd raycast/extension && ([ -d node_modules ] || npm install) && npm run build

# lint the extension with the Raycast eslint config
raycast-lint:
    cd raycast/extension && ([ -d node_modules ] || npm install) && npm run lint
