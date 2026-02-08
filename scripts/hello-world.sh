#!/bin/sh
set -eu

OUT_FILE="${BEAGLE_HELLO_OUT:-/tmp/beagle-hello-world.log}"
TS="$(date '+%Y-%m-%dT%H:%M:%S%z')"

echo "${TS} hello world" >> "$OUT_FILE"
