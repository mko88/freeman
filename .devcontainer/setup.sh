#!/usr/bin/env bash
# Installs everything needed to build and smoke-test the Wails GUI
# (cmd/freeman), including cross-compiling the Windows binary, all inside
# this devcontainer.
set -e

echo "Configuring git..."
# The workspace is bind-mounted from the Windows host, which owns the files
# under a different uid than the container's vscode user — without this, git
# refuses to operate on it at all ("detected dubious ownership").
git config --global --add safe.directory /workspaces/freeman
# The repo's shell scripts (build.sh, this file) must keep LF line endings
# regardless of which side (Windows host or Linux container) last touched
# them; core.autocrlf would otherwise risk converting them to CRLF, which
# bash can't execute ("bad interpreter" / silent script corruption).
git config core.autocrlf false

echo "Installing Node 22.x (Debian bookworm's apt nodejs is v18, too old for" \
	"the Vite 8 toolchain the frontend uses)..."
curl -fsSL https://deb.nodesource.com/setup_22.x | sudo -E bash -
sudo apt-get install -y nodejs

echo "Installing build dependencies..."
sudo apt-get update
sudo apt-get install -y \
	build-essential pkg-config \
	libgtk-3-dev libwebkit2gtk-4.0-dev \
	gcc-mingw-w64-x86-64 nsis \
	xclip xvfb imagemagick x11-apps xdotool
# libgtk/libwebkit2gtk: native Linux GUI target (webview)
# gcc-mingw-w64/nsis:   cross-compile + package the Windows target
# xclip:                clipboard support on Linux
# xvfb/imagemagick/x11-apps/xdotool: headless GUI smoke-testing (screenshot + simulated clicks)

echo "Installing Wails CLI..."
go install github.com/wailsapp/wails/v2/cmd/wails@latest

if [ -f go.mod ]; then
	echo "Downloading Go modules..."
	go mod download
fi

echo "Done. 'bash build.sh' now also produces bin/freeman and bin/freeman.exe."
