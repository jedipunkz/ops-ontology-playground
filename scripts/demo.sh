#!/usr/bin/env bash
# Runs Claude Code headless against the ops-ontology MCP server three times and
# checks the graph after each run. It recreates the ops database first, which
# deletes every recorded hypothesis. Each run uses your Claude Code quota.
set -euo pipefail

: "${TYPEDB_USERNAME:?set TYPEDB_USERNAME}"
: "${TYPEDB_PASSWORD:?set TYPEDB_PASSWORD}"
for command in claude go jq; do
  command -v "$command" >/dev/null || { echo "$command is required" >&2; exit 1; }
done

readonly incident="incident-5377"
readonly read_tools="mcp__ops-ontology__describe_ontology mcp__ops-ontology__search_operational_graph mcp__ops-ontology__list_hypotheses"
readonly write_tool="mcp__ops-ontology__record_root_cause_hypothesis"
readonly prompt="Why did checkout have a SEV-2 incident, and which deployment is a plausible cause? Record a hypothesis with the evidence from the graph. Reply in 3 lines or fewer."

# run_agent <allowed tools...> prints Claude's JSON result.
run_agent() {
  # dontAsk denies every tool call that is not allowlisted instead of
  # prompting, so the result does not depend on the user's default mode.
  claude -p "$prompt" \
    --model sonnet \
    --mcp-config .mcp.json --strict-mcp-config \
    --permission-mode dontAsk \
    --allowedTools "$@" \
    --output-format json
}

count_hypotheses() {
  go run ./cmd/hypotheses "$incident" | jq length
}

expect_count() {
  local want=$1 got
  got=$(count_hypotheses)
  if [[ "$got" != "$want" ]]; then
    echo "FAIL: $incident has $got hypotheses, want $want" >&2
    exit 1
  fi
  echo "OK: $incident has $got hypotheses"
}

go run ./cmd/seed

echo "== run 1: read tools only; the write must be denied"
# shellcheck disable=SC2086 # read_tools is a space-separated list.
result=$(run_agent $read_tools)
jq -r '.result' <<<"$result"
if ! jq -e --arg tool "$write_tool" '[.permission_denials[]?.tool_name] | index($tool)' <<<"$result" >/dev/null; then
  echo "FAIL: the agent did not attempt the write, or the write was not denied" >&2
  exit 1
fi
echo "OK: $write_tool was denied"
expect_count 0

echo "== run 2: all tools; the agent records a hypothesis"
# shellcheck disable=SC2086
jq -r '.result' <<<"$(run_agent $read_tools "$write_tool")"
expect_count 1

echo "== run 3: all tools; the agent reads run 2's hypothesis and does not duplicate it"
# shellcheck disable=SC2086
jq -r '.result' <<<"$(run_agent $read_tools "$write_tool")"
expect_count 1

go run ./cmd/hypotheses "$incident"
