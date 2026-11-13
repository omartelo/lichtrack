#!/bin/sh
# Builds what a lich release carries (lich's docs/extensions.md, Publishing a
# release): dist/<id>_<goos>_<goarch>.tar.gz for every target, each with
# manifest.json and the executable at its root, and dist/checksums.txt.
#
#   ./release.sh v0.1.0
set -eu

id=lichtrack
targets="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64"

version=${1:?usage: release.sh vX.Y.Z}
version=${version#v}
grep -q "\"version\": *\"$version\"" manifest.json || {
  echo "manifest.json does not say version $version: bump it before tagging" >&2
  exit 1
}

rm -rf dist
mkdir dist
for target in $targets; do
  goos=${target%/*}
  goarch=${target#*/}
  exe=$id
  # The manifest names the file lich runs, and on Windows that is the .exe.
  [ "$goos" = windows ] && exe=$id.exe
  stage=$(mktemp -d)
  CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build -trimpath -ldflags='-s -w' -o "$stage/$exe" .
  sed "s/\"executable\": *\"[^\"]*\"/\"executable\": \"$exe\"/" manifest.json > "$stage/manifest.json"
  cp LICENSE "$stage/"
  # Entries are named one by one: lich wants them at the archive's root.
  tar -C "$stage" -czf "dist/${id}_${goos}_${goarch}.tar.gz" manifest.json "$exe" LICENSE
  rm -rf "$stage"
done
(cd dist && sha256sum ./*.tar.gz | sed 's|\./||' > checksums.txt)
