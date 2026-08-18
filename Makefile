BIN := jasmr-dl
PKG := github.com/EagleStelle/jasmr-dl/cmd

# Resolved by make, not by the shell, so this works whether the recipe shell is
# sh or cmd.exe. $(shell) yields empty when git is missing or the tree is not a
# repository, which is the only case that falls back to "dev".
VERSION := $(shell git describe --tags --always --dirty)
ifeq ($(strip $(VERSION)),)
VERSION := dev
endif

# make on Windows runs recipes through cmd.exe unless sh.exe happens to be on
# PATH, so anything beyond a plain executable has to be spelled for both.
ifeq ($(OS),Windows_NT)
EXT    := .exe
RM     := cmd /c del /q
LIST   := cmd /c dir
DEVNUL := 2>NUL
else
EXT    :=
RM     := rm -f
LIST   := ls -lh
DEVNUL := 2>/dev/null
endif

# -s -w drop the symbol table and DWARF, which are 4.2 MB of the unstripped
# binary and are never needed at runtime; panic traces read pclntab and stay
# intact. -trimpath strips absolute build paths and makes the build reproducible.
LDFLAGS := -s -w -X $(PKG).version=$(VERSION)

.PHONY: build dev test clean install size

## build: release binary, stripped and reproducible
build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN)$(EXT) .

## dev: unstripped binary, keeps symbols so delve can attach
dev:
	go build -o $(BIN)$(EXT) .

## test: run the test suite
test:
	go test ./...

## install: release binary into GOBIN
install:
	go install -trimpath -ldflags="$(LDFLAGS)" .

## clean: remove build output
clean:
	go clean
	-$(RM) $(BIN)$(EXT) $(DEVNUL)

## size: report the size of the built binary
size: build
	@$(LIST) $(BIN)$(EXT)
