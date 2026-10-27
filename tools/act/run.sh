#!/bin/bash
# Local CI with act: tools/act/run.sh [act args], e.g. tools/act/run.sh -W .github/workflows/ci.yml -j web
#
# The job container (rfe-act, see Dockerfile) runs as root. .actrc therefore does not use --bind: act copies the
# checkout into the container, so nothing the jobs write (node_modules, dist, target, coverage) lands in this tree
# owned by root. The git dir is mounted read-only at its own path, so git steps work, also from a linked worktree
# whose .git file points outside the checkout, and a job cannot write to the host repository.
set -euo pipefail
top=$(git rev-parse --show-toplevel)
common=$(git -C "$top" rev-parse --path-format=absolute --git-common-dir)
cd "$top"
exec act --container-options "-v $common:$common:ro" "$@"
