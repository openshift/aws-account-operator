#!/usr/bin/env bash
set -uo pipefail

# Verify jq is available (required for JSON parsing)
if ! command -v jq &>/dev/null; then
  echo '{"decision": "block", "reason": "jq is not installed. Install jq to use prek validation hooks."}' >&2
  exit 1
fi

HOOK_INPUT=$(cat)

# Allow stop on retry to prevent infinite loops
STOP_HOOK_ACTIVE=$(echo "$HOOK_INPUT" | jq -r '.stop_hook_active // false')
if [[ "$STOP_HOOK_ACTIVE" == "true" ]]; then
  exit 0
fi

# Run prek validation
PREK_OUTPUT=$(prek run --all-files 2>&1)
PREK_EXIT=$?

if [[ $PREK_EXIT -eq 0 ]]; then
  exit 0
fi

# Block stop and tell Claude what to fix
jq -n \
  --arg reason "prek run --all-files failed. Fix the issues below, then try again:

$PREK_OUTPUT" \
  '{"decision": "block", "reason": $reason}'
