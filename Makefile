.PHONY: build studio test vet lint vulncheck fmt-check wasm wasm-check browser-check browser-test check demo

build:
	mkdir -p bin
	go build -trimpath -o bin/tegami ./cmd/tegami

# Write the offline Studio to build/tegami.html, as published in releases.
studio:
	mkdir -p build
	go run ./cmd/tegami studio --output build/tegami.html

test:
	go test ./...

vet:
	go vet ./...

# golangci-lint's default linters: errcheck, govet, ineffassign, staticcheck,
# and unused.
lint:
	golangci-lint run ./...
	actionlint

# Needs network access to the Go vulnerability database.
vulncheck:
	govulncheck ./...

fmt-check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }

# Argon2id for the browser pages: Monocypher compiled to WebAssembly with no C
# library. The module is committed and embedded; wasm-check confirms it
# rebuilds byte for byte with the clang pinned in flake.nix.
WASM_SOURCES = wasm/argon2.c wasm/monocypher/monocypher.c
WASM_FLAGS = --target=wasm32 -O2 -mbulk-memory -ffreestanding -nostdlib -Wall -Wextra \
	-Wl,--no-entry -Wl,--export=__heap_base -Wl,--max-memory=68157440 -Wl,--gc-sections -Wl,--strip-all

wasm:
	clang $(WASM_FLAGS) -o internal/envelope/assets/argon2.wasm $(WASM_SOURCES)

wasm-check:
	@mkdir -p build
	clang $(WASM_FLAGS) -o build/argon2.wasm $(WASM_SOURCES)
	cmp build/argon2.wasm internal/envelope/assets/argon2.wasm

check: fmt-check vet lint test build wasm-check

# Open the printed URL in Chromium; no browser automation dependencies needed.
browser-check: build
	go run ./scripts/browsercheck

# Run the same checks in headless Chromium and fail if any check fails. Set
# CHROMIUM to the browser binary; CI uses the Chrome on GitHub's runners.
CHROMIUM ?= chromium
browser-test: build
	go run ./scripts/browsercheck -chromium "$(CHROMIUM)"

# Record the README's terminal demo from a real session. See docs/development/setup.md.
demo: build
	@command -v vhs >/dev/null || { echo 'Install Charmbracelet VHS to record the demo.' >&2; exit 1; }
	@command -v jq >/dev/null || { echo 'Install jq to record the JSON example.' >&2; exit 1; }
	rm -rf bin/demo
	mkdir -p bin/demo/reports bin/demo/opened docs/assets
	printf 'region,revenue\nnorth,1200\nsouth,980\n' > 'bin/demo/Q3 forecast.csv'
	printf 'Board minutes, October.\n' > bin/demo/reports/minutes.txt
	head -c 3145728 /dev/urandom > bin/demo/reports/recording.bin
	PATH="$(CURDIR)/bin:$$PATH" vhs docs/demo.tape
