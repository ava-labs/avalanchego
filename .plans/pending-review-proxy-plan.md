# Pending Review Proxy Plan

## Goal

Enable the `pending-review` workflow from a Linux VM running on a macOS host
without giving the VM agent access to a privileged GitHub token.

The intended security model is capability restriction, not caller trust:

- the VM agent may manage pending-review drafts
- the VM agent must not gain a reusable write-capable GitHub credential
- the privileged GitHub auth remains on the macOS host
- the host should not need to run a general-purpose agent session

## Problem Statement

`gh-pending-review` currently shells out to local `gh auth token` using an
isolated `GH_CONFIG_DIR`, then talks directly to the GitHub API. That works on
the macOS host where YubiKey-backed login is possible, but it fails in the
Linux VM because the YubiKey cannot be passed through.

A token-forwarding design is not acceptable because it would hand the VM agent
a privileged token that could be reused for arbitrary GitHub API calls. The
solution must expose only a narrow pending-review capability.

## Proposed Architecture

Split the system into a VM-side client mode and a host-side proxy mode.

### VM side

`gh-pending-review` gains a proxy-client mode enabled by configuration, likely
through an env var such as `GH_PENDING_REVIEW_PROXY_URL`.

In proxy mode, the VM-side binary:

- parses and validates the normal CLI arguments
- serializes the command into a narrow JSON request
- sends that request to the proxy over HTTP via an SSH-forwarded localhost port
- renders the proxy response in the same stdout/stderr/exit-code shape expected
  by the existing skill and CLI users

In proxy mode, the VM-side binary must not:

- acquire a GitHub token locally
- access the GitHub API directly for remote pending-review operations
- expose a generic remote-command or token-retrieval path

### macOS host side

A small proxy service runs on the macOS host and listens on `127.0.0.1` only.
The Linux VM reaches it through SSH port forwarding.

The proxy:

- accepts only the supported pending-review operations
- reuses the existing `tools/pendingreview` app logic rather than reimplementing
  GitHub behavior
- performs GitHub auth locally on the host using the existing isolated
  `gh-pending-review` config
- keeps the pending-review local state on the host
- returns only command results, never the underlying auth material

### Transport

Use SSH-forwarded loopback transport:

- host proxy binds to `127.0.0.1:<port>`
- SSH forwards VM `127.0.0.1:<port>` to host `127.0.0.1:<port>`
- the VM client talks only to its own `127.0.0.1:<port>`

Binding to loopback on the host is preferred over binding to the VM bridge
because it minimizes exposure and makes SSH the only reachability path.

## Repo Split

### AvalancheGo

Keep the portable pending-review logic here:

- proxy request/response contract
- VM-side proxy client mode in `tools/pendingreview`
- proxy server and handler implementation in `tools/pendingreview`
- tests for proxy-mode behavior and contract compatibility
- documentation updates for the optional proxy transport

### Dotfiles

Put host-specific deployment and privilege-boundary machinery here:

- proxy service packaging and wrapper commands
- launchd or Home Manager service wiring
- SSH port-forward configuration
- host-local config and secret placement

This keeps workstation-specific service deployment out of the work monorepo
while still letting the core tool support the transport.

## Scope

### In scope

- add an optional proxy transport to `gh-pending-review`
- define a narrow protocol for the existing pending-review command surface
- ensure proxy mode never returns a privileged token to the VM
- keep remote pending-review state on the host
- document the operational model for host/VM use

### Out of scope

- exposing arbitrary `gh` commands remotely
- exposing a token endpoint
- generic GitHub proxying
- LAN-reachable service exposure
- changing the pending-review workflow semantics themselves

## Design Constraints

### Security constraints

- assume any process in the VM that can run `gh-pending-review` can also reach
  the forwarded localhost port
- therefore, the primary protection must be capability restriction at the proxy
  surface, not VM-side caller authentication
- the proxy must not expose any operation that can be repurposed into general
  GitHub write access

### Compatibility constraints

- the existing `pending-review` skill should continue to work with minimal or
  no instruction changes beyond proxy configuration
- local non-proxy use on the macOS host should continue to work unchanged
- in proxy mode, the client should proxy the full `gh-pending-review` command
  surface rather than splitting authority between VM-local and host-remote
  behavior
- proxy mode should be selected by environment, so the existing skill and
  launcher can continue to invoke the same `gh-pending-review` commands without
  skill-specific branching
- the proxy implementation and proxy integration suite should derive command
  coverage from one canonical command registry so new commands cannot silently
  miss proxy coverage

### Testing constraints

- fast deterministic tests should remain the default validation path
- any end-to-end host/VM deployment checks should be additive and explicitly
  gated

## Open Design Questions

1. Which commands should be proxied?

Recommended answer:

- in proxy mode, proxy the full `gh-pending-review` command set
- keep the mode model simple: local mode is fully local, proxy mode is fully
  proxied

2. Where should the proxy implementation live initially?

Recommended answer:

- client support and protocol definition in AvalancheGo
- deployable proxy service in dotfiles

3. How much app-layer authentication should the proxy require?

Recommended answer:

- SSH loopback forwarding is the main reachability control
- do not rely on VM-side identity for the main security property
- an optional shared secret header is acceptable as defense in depth but should
  not be treated as the core protection

4. How should client/server compatibility be enforced?

Recommended answer:

- every proxied request carries an exact proxy API version
- the server rejects any request whose version does not exactly match its own
- bump that version only when the proxy request/response API changes

5. Should the proxy enforce repo restrictions?

Recommended answer:

- yes, default to `ava-labs/avalanchego` only unless explicitly configured

6. How should proxy validation be staged?

Recommended answer:

- write proxy integration tests first, before implementation
- make those tests run locally on the macOS host without SSH or the Linux VM
- design the same suite so it can later run against a configured proxy that
  talks to actual GitHub under an explicitly gated live-test mode

7. How should we prevent new commands from missing proxy coverage?

Recommended answer:

- define a canonical command registry in `tools/pendingreview`
- use that registry for proxy routing and server allowlisting
- derive proxy integration coverage from that registry
- add a completeness test that fails if any proxyable command in the registry
  lacks a proxy integration case

## Implementation Phases

### Phase 1: Protocol and client mode in AvalancheGo

- write proxy integration tests first to establish the validation target
- define a canonical proxy command registry before adding routing logic
- define a JSON request/response protocol for the narrow command set
- add proxy mode selection to `tools/pendingreview`
- route all `gh-pending-review` commands over HTTP in proxy mode
- ensure proxy mode does not initialize local GitHub auth or a direct GitHub
  client for remote commands
- add an exact-match proxy API version field to every request
- add a completeness test that enforces proxy integration coverage for every
  proxyable command in the registry
- add deterministic tests for request construction, response decoding, and
  error/exit behavior

### Phase 2: Host proxy implementation

- implement a loopback-only proxy server in `tools/pendingreview`
- adapt requests into the existing `pendingreview.App` command paths
- keep host auth and host state local to the service
- reject version-mismatched requests before command execution
- add allowlist checks for supported commands and repo policy
- add deterministic server tests against fake backends where possible

### Phase 3: Dotfiles deployment

- package the proxy for local deployment
- add launchd or Home Manager config to keep it available on login
- add SSH forwarding config for the Linux VM
- document operational setup and failure recovery

### Phase 4: Integration validation

- validate the proxy locally on the macOS host without SSH or the Linux VM
- reuse the same proxy integration suite against a configured proxy talking to
  actual GitHub in an explicitly gated live mode
- after the local and live proxy paths are validated, validate one end-to-end
  VM-over-SSH workflow to confirm deployment wiring

## Test Matrix

The proxy integration suite should be the first new artifact written for this
work. It should exercise the CLI in proxy mode against a real proxy server, not
internal helper functions.

The suite should not hardcode an independent list of supported commands once the
registry exists. Instead, test case enumeration should be derived from the
canonical command registry, with payload-specific assertions attached per
command.

### Test modes

#### Mode A: local deterministic

- client: real `gh-pending-review` CLI path in proxy mode
- proxy: real proxy server implementation
- backend: fake GitHub backend
- transport: local `127.0.0.1` only, no SSH, no VM
- purpose: default fast validation for proxy semantics

#### Mode B: local live GitHub

- client: real `gh-pending-review` CLI path in proxy mode
- proxy: real proxy server implementation
- backend: actual GitHub
- transport: local `127.0.0.1` only, no SSH, no VM
- purpose: gated live validation of proxy behavior against real GitHub

#### Mode C: VM over SSH

- client: real `gh-pending-review` CLI path in proxy mode from the Linux VM
- proxy: real proxy server implementation on the macOS host
- backend: actual GitHub
- transport: SSH-forwarded localhost
- purpose: deployment validation only after Modes A and B pass

### Deterministic proxy integration cases

These should be implemented first in `tools/pendingreview`.

1. `version` in proxy mode returns the expected version output.
2. `create` in proxy mode creates one pending review and persists host-side
   state.
3. `get` in proxy mode returns the live pending review body and comments.
4. `update-body` in proxy mode updates the body when live state matches stored
   state.
5. `update-body` in proxy mode returns `ErrReviewConflict` when the live body
   diverges from stored state.
6. `replace-comments` in proxy mode creates the requested managed draft comment
   set.
7. `replace-comments` in proxy mode returns `ErrReviewCommentsConflict` on live
   divergence without `--force`.
8. `upsert-comment --create-if-missing` in proxy mode creates a pending review
   when absent and writes the requested comment.
9. `upsert-comment` by `--comment-id` in proxy mode updates exactly the
   targeted managed comment.
10. `delete` in proxy mode removes the live pending review and clears stored
    state.
11. `delete --ensure-absent` in proxy mode succeeds when no live review exists
    and still clears stored state.
12. `get-state` in proxy mode reads the authoritative host-side state.
13. `delete-state` in proxy mode deletes the authoritative host-side state.
14. default repo restriction rejects repos other than `ava-labs/avalanchego`
    unless explicitly configured.
15. request version mismatch fails before command execution with a clear error.
16. proxy mode does not attempt local GitHub auth token acquisition for proxied
    commands.

Add one completeness test:

- enumerate all proxyable commands from the canonical registry
- enumerate all commands covered by the proxy integration suite
- fail if the two sets differ

### Gated live proxy cases

These should reuse the same client/proxy path and as much of the same harness
shape as possible, differing only in backend configuration.

1. `create` on a user-selected PR.
2. `update-body` followed by conflict detection after an out-of-band body edit.
3. `replace-comments` on a user-selected anchor.
4. `upsert-comment --create-if-missing`.
5. `delete --ensure-absent`.

Existing live-test guardrails should carry over:

- require explicit env gating
- require user-selected PR inputs
- require explicit comment anchor inputs for comment tests
- never pick a PR autonomously

### VM-over-SSH validation cases

Keep this set intentionally small:

1. One `create` flow from the VM through an SSH-forwarded localhost port.
2. One `get` flow from the VM through the same path.
3. One conflict-preservation flow showing that the transport wiring does not
   change optimistic-concurrency behavior.

## Proxy Request Sketch

The first implementation should use one HTTP endpoint and one exact-match API
version field carried on every request.

### Transport

- method: `POST`
- path: `/pending-review`
- content type: `application/json`
- listen address: `127.0.0.1:<port>`

### Request envelope

```json
{
  "version": 1,
  "command": "create",
  "payload": {}
}
```

Rules:

- `version` is an exact-match proxy API version, not a generic build version
- the server rejects the request if `version` does not exactly match
- `command` must be one of the supported `gh-pending-review` subcommands
- `payload` shape depends on `command`

### Response envelope

```json
{
  "stdout": "",
  "stderr": "",
  "exit_code": 0
}
```

Rules:

- the proxy client should render `stdout` and `stderr` as if the local command
  had produced them
- `exit_code` controls the client-side process exit semantics
- the proxy should not return auth material or debug-only token details

### Command payload sketch

The initial payload shape should mirror the parsed command structs closely so
the server can route through existing app logic with minimal translation.

Exception:

- file-path flags such as `--body-file` and `--comments-file` must be resolved
  on the client before the request is sent
- the proxy boundary must carry content, not caller-local file paths, because
  the host proxy cannot read temp files that exist only on the VM

#### `create`

```json
{
  "repo": "ava-labs/avalanchego",
  "pr_number": 123,
  "body": "text",
  "config_dir": "/Users/me/.config/gh-pending-review",
  "state_dir": "/Users/me/.local/state/gh-pending-review",
  "json": true
}
```

#### `get`

```json
{
  "repo": "ava-labs/avalanchego",
  "pr_number": 123,
  "config_dir": "/Users/me/.config/gh-pending-review",
  "state_dir": "/Users/me/.local/state/gh-pending-review",
  "pretty": false
}
```

#### `update-body`

```json
{
  "repo": "ava-labs/avalanchego",
  "pr_number": 123,
  "body": "text",
  "config_dir": "/Users/me/.config/gh-pending-review",
  "state_dir": "/Users/me/.local/state/gh-pending-review",
  "force": false,
  "json": true
}
```

#### `replace-comments`

```json
{
  "repo": "ava-labs/avalanchego",
  "pr_number": 123,
  "comments": [],
  "config_dir": "/Users/me/.config/gh-pending-review",
  "state_dir": "/Users/me/.local/state/gh-pending-review",
  "force": false,
  "create_if_missing": false,
  "review_body": "",
  "json": true
}
```

#### `upsert-comment`

```json
{
  "repo": "ava-labs/avalanchego",
  "pr_number": 123,
  "comment_id": "",
  "path": "foo.go",
  "line": 10,
  "side": "RIGHT",
  "start_line": 0,
  "start_side": "",
  "body": "text",
  "config_dir": "/Users/me/.config/gh-pending-review",
  "state_dir": "/Users/me/.local/state/gh-pending-review",
  "force": false,
  "create_if_missing": true,
  "review_body": "",
  "json": true
}
```

#### `delete`

```json
{
  "repo": "ava-labs/avalanchego",
  "pr_number": 123,
  "config_dir": "/Users/me/.config/gh-pending-review",
  "state_dir": "/Users/me/.local/state/gh-pending-review",
  "ensure_absent": true,
  "json": true
}
```

#### `get-state`

```json
{
  "repo": "ava-labs/avalanchego",
  "pr_number": 123,
  "user_login": "maru",
  "state_dir": "/Users/me/.local/state/gh-pending-review",
  "pretty": false
}
```

#### `delete-state`

```json
{
  "repo": "ava-labs/avalanchego",
  "pr_number": 123,
  "user_login": "maru",
  "state_dir": "/Users/me/.local/state/gh-pending-review",
  "json": true
}
```

#### `version`

```json
{}
```

### Initial implementation note

For proxy mode, the client should still preserve the existing user-facing CLI
boundary, but it must resolve file-based flags into concrete content before
making the HTTP request.

That means:

- `--body-file` becomes `body`
- `--review-body-file` becomes `review_body`
- `--comments-file` becomes structured `comments`

This keeps the CLI contract unchanged for callers while avoiding broken
host/VM filesystem assumptions at the proxy boundary.

## Risks

### Hidden token exposure

If the proxy returns auth-related error detail, debug logging, or helper
endpoints, it may accidentally leak privileged material. The proxy contract
must stay intentionally narrow.

### State split-brain

If remote GitHub operations run on the host while state remains in the VM, the
conflict-detection model becomes unreliable. Remote mode should use host-side
state as the source of truth.

### CLI compatibility drift

If proxy mode changes stdout, stderr, or exit-code behavior, the existing skill
and tests may drift. The proxy client should preserve the current CLI contract
as closely as possible.

### Repo-boundary confusion

If the deployable daemon lands in AvalancheGo too early, workstation-specific
service concerns may become coupled to the repo tool. Keep deployment in
dotfiles until there is clear multi-user demand.

## Acceptance Criteria

### Functional

- From the Linux VM, `gh-pending-review create --pr <n> ...` succeeds in proxy
  mode without requiring local `gh auth login`.
- In proxy mode, the full `gh-pending-review` command set works through the
  proxy with the same user-level behavior expected from direct mode.
- The `pending-review` skill can use proxy mode without changing its supported
  workflow semantics or requiring skill-specific code changes.
- Remote-mode stateful operations preserve the existing optimistic-concurrency
  protections against overwriting human GitHub edits.
- Requests from a client with a non-matching proxy API version fail immediately
  with a clear compatibility error.
- Proxy mode can be enabled entirely through environment configuration, so the
  same CLI invocation works in direct and proxied modes.

### Security

- The VM agent never receives a privileged GitHub token in command output,
  environment, on-disk config, or proxy responses.
- The proxy exposes only the pending-review capability set and cannot be used as
  a generic GitHub or `gh` execution channel.
- The proxy is reachable only through localhost binding plus SSH forwarding, not
  via a LAN-exposed interface.
- In proxy mode, the VM-side implementation does not make direct GitHub API
  calls for proxied commands.

### Operational

- The macOS host proxy can be started automatically via dotfiles-managed
  service configuration.
- The Linux VM can reach the proxy through existing SSH-forwarding patterns.
- Failure modes are legible: proxy unavailable, SSH tunnel missing, host auth
  expired, and optimistic-concurrency conflicts produce actionable errors.

### Validation

- Deterministic AvalancheGo tests cover proxy client behavior and proxy-server
  request handling.
- A proxy integration suite in `tools/pendingreview` is written before the
  proxy implementation and exercises the normal command surface through a real
  proxy server.
- That proxy integration suite runs locally on the macOS host without requiring
  SSH or the Linux VM.
- The same suite is designed to run in an explicitly gated live mode against a
  configured proxy talking to actual GitHub.
- At least one explicit end-to-end host/VM manual validation path is documented
  after local and live proxy validation pass.
- Validation includes a negative check demonstrating that the VM cannot use the
  proxy path to obtain a reusable privileged token.

## Initial Validation Plan

Before considering the design complete, validate these scenarios:

1. Local macOS proxy validation of the full command surface against a fake
   backend, without SSH or the Linux VM.
2. Local macOS proxy validation in explicitly gated live mode against a
   user-selected PR on actual GitHub.
3. Body update after a prior proxied create.
4. Conflict detection after the pending review is edited in GitHub between
   proxied runs.
5. Comment-only creation via `upsert-comment --create-if-missing`.
6. Negative attempt to use the proxy as a generic GitHub credential source.

## Recommended Next Step

Start with Phase 1 in AvalancheGo: define the proxy contract and add VM-side
proxy client mode, because that creates the narrow interface the dotfiles-hosted
service must implement and keeps the workstation-specific deployment work
separate.
