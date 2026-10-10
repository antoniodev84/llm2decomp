#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

if [[ -z "${LLM_API_KEY:-}" && -z "${OPENAI_API_KEY:-}" ]]; then
  echo "error: set LLM_API_KEY (or OPENAI_API_KEY)" >&2
  exit 1
fi

exec go run . "$@"
