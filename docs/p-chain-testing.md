# P-Chain testing

This document explains how P-Chain behavior is tested through public APIs in
integration, e2e, and Antithesis tests, and which tests to add when a
transaction or query changes. This spike proposes a foundation for P-Chain
test coverage: the tooling tracked in
[#5700](https://github.com/ava-labs/avalanchego/issues/5700) and
[#5701](https://github.com/ava-labs/avalanchego/issues/5701), the sharing
between them that [#5702](https://github.com/ava-labs/avalanchego/issues/5702)
investigates, and this layout as the standard for new P-Chain tests.

## Table of contents

- [Principles](#principles)
- [How the layers divide the work](#how-the-layers-divide-the-work)
  - [Shared tooling](#shared-tooling)
- [Checklist for a new transaction or query](#checklist-for-a-new-transaction-or-query)
  - [Integration](#integration)
  - [e2e](#e2e)
  - [Antithesis](#antithesis)
- [How the harnesses work](#how-the-harnesses-work)
- [Design decisions](#design-decisions)
- [Validation](#validation)

## Principles

- **Test through public APIs.** Issue transactions through the wallet and read
  state through RPC responses. A test that reads VM fields or the database
  breaks when internal types, package boundaries, or database layout change,
  which is the change these tests must survive.
- **Use each layer for the claim it can prove.** Integration checks public
  behavior against one VM, including acceptance, state, fees, rewards, and
  persistence. e2e repeats representative behavior across nodes and covers
  propagation and process lifecycle. Antithesis checks consistency while
  workers issue transactions under faults.
- **Share actions and expectations, not harnesses.** Integration and e2e reuse
  actions, commitment waits, and state verifiers from
  [`tests/fixture/pchain`](../tests/fixture/pchain). The same verifier checks
  one node in integration and every node in e2e. Antithesis reuses the actions
  but observes decisions through a fault-tolerant monitor. Each layer keeps its
  own runtime, so their independent lifecycles expose different failures.

These are defaults, not absolute rules. Choose a different layer when it makes
a claim easier to prove or a failure easier to diagnose.

## How the layers divide the work

| Layer                                                                          | Runtime                               | Proves                                                                   | Does not cover                          |
| ------------------------------------------------------------------------------ | ------------------------------------- | ------------------------------------------------------------------------ | --------------------------------------- |
| Unit ([`vms/platformvm`](../vms/platformvm))                                   | Executors and state in-process        | Protocol math, serialization, every rejection rule                       | Public API contracts                    |
| Integration ([`tests/integration/p`](../tests/integration/p))                  | One PlatformVM behind its RPC handler | Acceptance, exact balances, state, time boundaries, persistence, queries | Multi-node claims                       |
| e2e ([`tests/e2e/p`](../tests/e2e/p))                                          | Multi-node tmpnet network             | Agreement on every node, restart, bootstrap, new network paths           | Exhaustive rejection and fee matrices   |
| Antithesis ([`tests/antithesis/avalanchego`](../tests/antithesis/avalanchego)) | Workers issuing under faults          | Recorded decisions stay consistent while monitored                       | Healthy-network verifiers, exact values |

Integration tests run in the unit suite because they are hermetic: one VM on
an in-memory database, with no network and no built binary. Keep them that
way.

This spike covers AddValidator and AddDelegator in integration, e2e, and
Antithesis. Integration also covers transaction status and delegation at its
end time. The checklist below guides additional transaction and query coverage.

### Shared tooling

[`tests/fixture/pchain`](../tests/fixture/pchain) provides:

- **Nodes** ([`network.go`](../tests/fixture/pchain/network.go)): a P-Chain
  client labeled by node ID, plus a concurrent fan-out over a node list.
- **Actions** ([`staking.go`](../tests/fixture/pchain/staking.go)): issue one
  transaction through the wallet and return the transaction and an error.
  Actions take only what tests vary and discover the rest, such as the minimum
  stake, from the issuing node.
- **The commitment wait**: every node reports `Committed` in the same round
  before the test context's default timeout.
- **Verifiers**: compare the transaction bytes and resulting state on every
  node against the transaction itself.
- **Compound helpers** such as `AddValidator`: issue, wait, and verify in one
  call. Integration and e2e tests use these unless they need a step in
  between.

The single-VM harness lives in [`vm.go`](../tests/fixture/pchain/vm.go).
[`tests/tb_test_context.go`](../tests/tb_test_context.go) adapts `testing.TB`
to `tests.TestContext`, so the shared helpers run under `go test` as well as
Ginkgo.

## Checklist for a new transaction or query

Work through the layers in order. Every change gets integration coverage. A
transaction also gets one e2e spec. A transaction joins the Antithesis
workload when it can discover its inputs from public state and tolerate
concurrent activity. The staking tests in each layer are working examples.

### Integration

1. Put the test in the domain file that owns the state it touches, such as
   `staking_test.go` or `tx_test.go`, or start a new domain file such as
   `subnet_test.go`. A query test lives with the transactions that produce
   what it reads.
2. For a transaction, add its action and verifier to the matching domain file
   in [`tests/fixture/pchain`](../tests/fixture/pchain).
3. Add a compound helper that issues, waits, and verifies.
4. Cover the happy path: issue, wait, verify, and assert the exact balance
   change from the transaction's inputs and outputs.
5. Cover persistence: reopen the VM and verify the status and state again.
6. Cover the time boundaries the transaction depends on. Advance chain time
   past staker end times so the VM produces the resulting system
   transactions. Add a harness option when a test needs a different upgrade
   schedule.
7. For a query, cover known and unknown IDs, empty and populated results,
   pagination where exposed, pinned heights, and results after reopening.
8. Cover selected rejections through the wallet or RPC. Keep the exhaustive
   rejection matrix in the owning unit tests.

### e2e

1. Add one happy-path spec. Issue through a random node and call the compound
   helper with every node in the network.
2. Add a rejection case only when the network adds a claim that one VM cannot
   prove, such as node-specific or changing network state. Keep repetitive
   rejection cases in integration or unit tests.
3. Add a second spec only when the transaction opens a new network path: a
   chain that must start on tracking nodes, a Warp message, cross-chain shared
   memory, or state that must survive a node restart. Assertions on a created
   chain cover only the nodes that track its subnet.
4. Create state unique to the spec. Use `ginkgo.Serial` or a private network
   when the spec needs exclusive global state.

### Antithesis

1. Add a thin action in
   [`pchain.go`](../tests/antithesis/avalanchego/pchain.go). Call the shared
   action within the action timeout and hand the result to the monitor through
   `recordPChainResult`.
2. Add the action to the worker's action list in
   [`main.go`](../tests/antithesis/avalanchego/main.go). Give it a run cap
   when unbounded runs would exhaust the worker's P-Chain funding or threaten
   quorum, as validators with made-up node IDs would. Delegations to real
   validators need no cap.
3. Nodes can agree on blocks and still disagree on state if one applies a
   transaction wrongly. If the transaction creates state that the monitor's
   block and primary validator set comparisons do not cover, such as L1
   validator weights or subnet owners, add a monitor check that compares that
   state across nodes at a common height.
4. Check funding. Setup funds each worker on the X-Chain and P-Chain from the
   genesis key. Raise the per-worker P-Chain funding in `main.go` if the new
   action needs more.

## How the harnesses work

**Single VM.** One PlatformVM runs on an in-memory database behind its real
RPC handler, with local network parameters and every upgrade active. The
public client and wallet work unchanged. Methods on the harness stand in for
the events a network would produce: a block is accepted whenever transactions
are pending, chain time advances on demand, acceptance can be paused to
observe processing transactions, and the VM can be reopened on the same
database to prove persistence. The comments in
[`vm.go`](../tests/fixture/pchain/vm.go) describe each method's constraints.

**Network.** e2e specs share one tmpnet network. Every node tracks the P-Chain,
so the commitment wait requires every running, non-ephemeral node to report
commitment in the same polling round. That is the agreement claim e2e exists
to make. A spec that starts an ephemeral node must query it explicitly.

**Antithesis.** Each worker issues through one node and hands every transaction
produced by a P-Chain action to a shared monitor. The monitor runs in its own
goroutine, so a stalled worker does not stop its checks. Each round it checks
that no node's accepted height decreases and that reachable nodes agree on the
block and primary validator set at a common height. For each tracked
transaction it checks that no node reports it aborted, that a node that
reported it committed keeps doing so, and that committed bytes hash to its ID.
An unreachable node is never evidence.

The committed-stays-committed check covers a transaction only until every
node has verified it; after that, only the height and block checks remain.

The monitor checks generic decision and agreement properties; integration and
e2e verify transaction-specific effects.

## Design decisions

- **Actions return the transaction alongside the error.** Submission may have
  reached the node before a timeout. Integration and e2e treat any error as
  failure. Antithesis records the transaction so the monitor can still observe
  it, and rebuilds its wallet so spent inputs are not reused.
- **Antithesis does not run the verifiers.** The verifiers require every node
  to answer at once, which faults make impossible, so the monitor accumulates
  observations across rounds instead.
- **Verifiers derive expectations from the transaction.** Callers cannot pass
  expectations that disagree with what was issued, and the same verifier
  serves every parameterization.

## Validation

To run the P-Chain integration tests:

```bash
go test ./tests/integration/...
```

To run the P-Chain e2e specs:

```bash
task test-e2e -- --ginkgo.label-filter=p
```

To run the Antithesis workload against a local network for two minutes,
build a race-enabled node and run the workload:

```bash
./scripts/build.sh -r
```

```bash
go run ./tests/antithesis/avalanchego --avalanchego-path=./build/avalanchego --duration=120s
```

`task test-build-antithesis-images-avalanchego` runs the same workload and then
builds the Antithesis Docker images, so it also needs Docker.

Check the workload log for `issued P-chain transaction` and `checked P-chain
transactions` entries. They show that the staking actions ran and that the
monitor observed them.
