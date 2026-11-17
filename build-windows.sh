#!/bin/sh
# Cross-build the Windows editions of Trayscale from Linux/macOS.
#
# Usage:
#   ./build-windows.sh
#
# Produces standalone, no-console binaries in dist/:
#   Trayscale-GUI.exe  - full window (Gio), machine sidebar + detail pane
#   Trayscale.exe      - system-tray icon + menu (fyne.io/systray)
# and a console diagnostic:
#   Trayscale-Doctor.exe - prints LocalAPI status/watch results
#
# Both GUI editions reuse the same tsutil core and need no CGO / GTK. Requires
# Go >= 1.26.5. The checked-in rsrc_windows_amd64.syso files embed the required
# application manifest and icon; see build-windows.ps1 for how to regenerate
# them.
set -eu

cd "$(dirname "$0")"

export CGO_ENABLED=0
export GOOS=windows
export GOARCH=amd64

mkdir -p dist

echo 'Building dist/Trayscale-GUI.exe (Gio window) ...'
go build -mod=mod -trimpath -ldflags '-s -w -H=windowsgui' -o dist/Trayscale-GUI.exe ./cmd/trayscale-gui

echo 'Building dist/Trayscale.exe (tray) ...'
go build -mod=mod -trimpath -ldflags '-s -w -H=windowsgui' -o dist/Trayscale.exe ./cmd/trayscale-win

echo 'Building dist/Trayscale-Doctor.exe (console diagnostic) ...'
go build -mod=mod -trimpath -ldflags '-s -w' -o dist/Trayscale-Doctor.exe ./cmd/trayscale-doctor

for f in dist/Trayscale-GUI.exe dist/Trayscale.exe dist/Trayscale-Doctor.exe; do
	printf 'OK: %s (%s)\n' "$f" "$(du -h "$f" | cut -f1)"
done
