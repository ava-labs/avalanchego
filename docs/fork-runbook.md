# Fork Mode Runbook

Fork mode splits a private network off Mainnet or Fuji at a scheduled time `T`.
Design: `FORK.md`. Only fork nodes are affected; the source network never is.

## 1. Plan

- Pick `T` with enough lead time for every fork node to sync (C-Chain state
  sync takes hours). Pick `Δ` (`gracePeriod`, default `61s`).
- Generate one staking TLS key and one BLS key **per node**. Never share keys
  between nodes.
- For each validator, collect: node ID, weight, BLS `signer`
  (`info.getNodeID` returns `nodePOP` with `publicKey` and `proofOfPossession`),
  and a staking IP:port reachable by the other fork nodes.

## 2. Write the config

```json
{
  "forkTime": "2026-10-15T15:00:00Z",
  "gracePeriod": "61s",
  "validators": [
    {"nodeID": "NodeID-...", "weight": 100,
     "signer": {"publicKey": "0x...", "proofOfPossession": "0x..."},
     "ip": "10.0.0.1:9651"}
  ]
}
```

Every fork node gets the identical file (`--fork-config-file=<path>`). Alternatively, the same JSON can be passed inline, base64-encoded, using `--fork-config-file-content=<base64>`; if both flags are set, the file takes precedence. Before `T`,
restarting a node with a changed file re-arms it with the new config; from `T` on, a
changed file makes the node refuse to start.

Optional: `--upgrade-file` may reschedule only activations that are at or after `T`
both before and after the change.

## 3. Before T

- Start every fork node with its normal network flags plus `--fork-config-file`.
- Confirm on each node:
  - `health.health` → check `fork`: `phase` is `observing`.
  - `info.isBootstrapped` is true for P, X, and C.
  - `info.peers` includes every other fork node.
- A node started after `T` without having armed refuses to start (`late joiners
  are not supported`).

## 4. At T and T+Δ

Nothing to do. At `T+Δ` each node logs `fork switchover complete` and drops every
non-fork peer. Check `fork` health: `phase` is `switched`.

## 5. After T+Δ

- **Issue any P-chain transaction** (for example a tiny `BaseTx`). Until the fork's
  P-chain accepts a block at or after `T`, `H_fork` is unknown and Warp still
  expects the source validators. The `fork` health check turns unhealthy 10 minutes
  after `T+Δ` if this hasn't happened. One way to issue one is the Go wallet example
  `wallet/subnet/primary/examples/create-subnet` (a `CreateSubnetTx`), pointed at a
  fork node's API URI and a funded key; `avalanche-cli` works too.
- Issue an X- and a C-chain transaction so every chain records its fork point.
- Record each node's `fork` health report (`forkHeight`, `forkPoints`). They must be
  identical on every node.
- A fork node that restarts after `T` and has to state-sync the C-Chain (because it
  was offline past the state-sync threshold) will not report a C-Chain fork point;
  compare fork points only among nodes that followed the chain through `T`.
- Warp signatures from fork validators verify roughly one epoch (5 minutes on
  Mainnet/Fuji) after `H_fork`.

## 6. Fuji rehearsal

Run steps 1–5 on Fuji with 3–5 fork nodes before any Mainnet fork. In addition:
- Restart one fork node after `T+Δ` and confirm it returns to `switched` with the
  same fork points.
- Send a Warp message from the fork's C-Chain and verify it on the fork's P-Chain or
  another fork chain. This is the end-to-end Warp check that the automated suite
  does not cover.

## Defense in depth

Firewall rules limiting fork nodes to each other are recommended but not required:
the node isolates itself at `T+Δ`.
