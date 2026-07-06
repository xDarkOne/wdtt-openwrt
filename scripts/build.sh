#!/bin/sh
# Cross-compile the wdtt-client binary for OpenWrt targets.
#
# Uses the official golang docker image so no Go toolchain is needed on the
# host. Run from anywhere; outputs static binaries into ./dist.
#
#   scripts/build.sh                 # builds arm64 (default; all Xiaomi/Cudy)
#   scripts/build.sh arm64 armv7 mipsle amd64
set -e

cd "$(dirname "$0")/.."
mkdir -p dist

IMAGE="${WDTT_GO_IMAGE:-golang:1.25}"
TARGETS="${*:-arm64}"

echo "Building targets: $TARGETS (image: $IMAGE)"

docker run --rm -v "$PWD":/src -w /src -e GOFLAGS=-buildvcs=false \
	-e "TARGETS=$TARGETS" "$IMAGE" sh -ec '
	go mod tidy
	for t in $TARGETS; do
		unset GOARM GOMIPS
		case "$t" in
			arm64)  export GOARCH=arm64;  OUT=dist/wdtt-client-linux-arm64 ;;
			armv7)  export GOARCH=arm GOARM=7; OUT=dist/wdtt-client-linux-armv7 ;;
			mipsle) export GOARCH=mipsle GOMIPS=softfloat; OUT=dist/wdtt-client-linux-mipsle ;;
			mips)   export GOARCH=mips GOMIPS=softfloat; OUT=dist/wdtt-client-linux-mips ;;
			amd64)  export GOARCH=amd64; OUT=dist/wdtt-client-linux-amd64 ;;
			*) echo "unknown target: $t"; exit 1 ;;
		esac
		CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o "$OUT" ./cmd/wdtt-client
		echo "built $OUT"
	done
	# Server-side links endpoint (VPS is amd64).
	unset GOARM GOMIPS
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" \
		-o dist/wdtt-linkd-linux-amd64 ./cmd/wdtt-linkd
	echo "built dist/wdtt-linkd-linux-amd64"
'

ls -la dist
