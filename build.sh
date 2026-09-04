#!/usr/bin/env bash
# Builds both Windows and Linux desktop GUI binaries by default. Pass
# --windows or --linux to build only that platform — --windows for routine
# use on the Windows host (which never runs the Linux binary), --linux when
# only a Linux binary is needed (e.g. Xvfb-based visual verification of the
# GUI in the dev container).
#
# go vet, go test, and svelte-check are opt-in via --vet/--test/--check (or
# --full for all three) — they're not part of the default run since most
# changes don't need all of them.
#
# --server is also opt-in: it builds cmd/freeman-server (the headless
# HTTP/container counterpart to the desktop GUI) instead of/alongside the
# desktop build — needs neither the Wails CLI nor mingw-w64, only Go and
# Node (for the separate "web" frontend bundle it embeds).
set -e

build_windows=1
build_linux=1
build_server=0
run_vet=0
run_test=0
run_check=0

for arg in "$@"; do
	case "$arg" in
		--windows) build_linux=0 ;;
		--linux) build_windows=0 ;;
		--server) build_server=1 ;;
		--vet) run_vet=1 ;;
		--test) run_test=1 ;;
		--check) run_check=1 ;;
		--full) run_vet=1; run_test=1; run_check=1 ;;
	esac
done

if [ "$run_vet" = 1 ]; then
	echo "Running go vet..."
	go vet ./...
fi

if [ "$run_test" = 1 ]; then
	echo "Running go test..."
	go test ./...
fi

if [ "$run_check" = 1 ]; then
	echo "Running svelte-check..."
	(cd cmd/freeman/frontend && npm run check)
fi

mkdir -p bin

if [ "$build_linux" = 1 ]; then
	if command -v wails &> /dev/null; then
		echo "Building GUI (linux/amd64)..."
		(cd cmd/freeman && wails build)
		cp cmd/freeman/build/bin/freeman bin/
	else
		echo "wails CLI not found — skipping Linux build (see README for setup)"
	fi
fi

if [ "$build_windows" = 1 ]; then
	if command -v wails &> /dev/null && command -v x86_64-w64-mingw32-gcc &> /dev/null; then
		echo "Building GUI (windows/amd64)..."
		(cd cmd/freeman && GOOS=windows GOARCH=amd64 CGO_ENABLED=1 \
			CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
			wails build -platform windows/amd64)
		cp cmd/freeman/build/bin/freeman.exe bin/
	else
		echo "wails CLI or mingw-w64 not found — skipping Windows cross-compile (see README)"
	fi
fi

if [ "$build_server" = 1 ]; then
	echo "Building web frontend..."
	(cd cmd/freeman/frontend && npm run build:web)

	echo "Populating cmd/freeman-server/web/..."
	find cmd/freeman-server/web -mindepth 1 ! -name '.gitignore' -delete
	cp -r cmd/freeman/frontend/dist-web/. cmd/freeman-server/web/

	echo "Building freeman-server (linux/amd64)..."
	(cd cmd/freeman-server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../../bin/freeman-server .)
fi

echo "Done."
ls -lh bin/
