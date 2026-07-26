---
tracker:
  kind: github
  provider:
    repo: FuqingZh/agent-orchestrator
    token: $AO_GITHUB_TOKEN
  required_labels:
    - Symphony
  active_states:
    - Open
    - In Progress
  terminal_states:
    - Done
    - Cancelled
polling:
  interval_ms: 15000
workspace:
  root: $WORKSPACE_ROOT
hooks:
  after_create: |
    git status --short
  before_run: |
    git fetch origin
  after_run: |
    git status --short
  before_remove: |
    git status --short
  timeout_ms: 45000
agent:
  max_concurrent_agents: 4
  max_turns: 12
  max_retry_backoff_ms: 120000
  max_concurrent_agents_by_state:
    In Progress: 2
    Review: 1
    invalid-zero: 0
    invalid-text: many
codex:
  command: codex app-server
  approval_policy: never
  thread_sandbox: workspace-write
  turn_sandbox_policy:
    type: workspaceWrite
  turn_timeout_ms: 900000
  read_timeout_ms: 4000
  stall_timeout_ms: 180000
extension:
  retained: true
---
# {{ issue.identifier }}

{{ issue.title }}

Attempt: {{ attempt }}
