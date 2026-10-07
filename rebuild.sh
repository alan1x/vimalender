#!/bin/sh
# Rebuild the locally patched vimalender and install it into ~/go/bin.
set -e
cd "$(dirname "$0")"
out="$HOME/go/bin/.vimalender.new"
go build -o "$out" .
mv -f "$out" "$HOME/go/bin/vimalender"
echo "OK: $HOME/go/bin/vimalender rebuilt and installed."
