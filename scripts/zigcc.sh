#!/usr/bin/env bash
# Serializes zig cc invocations behind a flock.
#
# cgo compiles several synthesized C files per package concurrently within a
# single `go build`, which triggers a known Zig cache-concurrency bug
# ("error: unable to create compilation: AccessDenied") when multiple `zig
# cc` processes share one cache dir at the same time. Wrapping every
# invocation in a flock on a fixed lockfile serializes them and avoids the
# race, at the cost of some parallelism during CGO compilation only (not the
# rest of the Go build).
set -euo pipefail

lockfile="${ZIGCC_LOCKFILE:-${TMPDIR:-/tmp}/aidw-zigcc.lock}"

exec flock "$lockfile" zig cc "$@"
