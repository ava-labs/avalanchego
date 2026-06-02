# Snow Consensus & Consensus Engines

The `snow/` tree implements AvalancheGo's **Snow consensus family** (Slush → Snowflake → Snowball → Snowman) together with the **consensus engines** that drive it, the **bootstrapping** and **state-sync** state machines that get a node to the network tip, and the **networking glue** (handler, router, sender, timeout manager, benchlist) that turns inbound P2P messages into Accept/Reject decisions and outbound queries back to peers. This document describes how the per-chain pipeline `P2P → ChainRouter → Handler → Engine → Consensus + VM + Sender` works, the voting algorithms it runs, and the precise component boundaries between these pieces. (Simplex consensus is covered separately in [Simplex](simplex.md); the VM contract in [VM Framework](vm-framework.md); the P2P transport in [Networking](networking.md).)

---

## 1. Responsibilities & scope

- **Decision algorithms** (`snow/consensus/*`): repeated network sampling that converts noisy poll results into irreversible Accept/Reject decisions with quantifiable safety.
  - `snowball`: the binary/unary/n-ary Slush/Snowflake/Snowball primitives and the patricia-`Tree` that decides among conflicting IDs by shared bit prefix.
  - `snowman`: linear-chain consensus — a `Topological` tree of per-block Snowball instances that amortizes a single network poll across an entire branch.
  - `avalanche` / `snowstorm`: legacy DAG types (now vestigial — the X-Chain runs Snowman since Cortina); only thin `vertex.go` / `tx.go` interfaces remain.
- **Engines** (`snow/engine/*`): the per-chain loop. In normal operation the Snowman `Engine` issues blocks into consensus, samples validators, collects chits, and applies votes. Before that it runs a `stateSyncer` and/or `Bootstrapper` to reach the tip.
- **Networking** (`snow/networking/*`): `ChainRouter` (routes by ChainID, owns adaptive request timeouts), `Handler` (sync/async message queues feeding the engine under the chain lock), `sender` (registers timeouts, wraps the network), `timeout.Manager`, `benchlist`, and resource `tracker`.
- **Shared core**: `snow.ConsensusContext`, `snow.Decidable`, `snow.Acceptor`, engine state machine (`snow/state.go`), `validators` (subnet validator sets sourced from the P-Chain), and `uptime` tracking.

**Out of scope here:** Simplex (`simplex/`, `snow/consensus/simplex/`), VM-internal logic, and the network transport/TLS layer.

---

## 2. Package / file layout

| Area | Path | Purpose |
|------|------|---------|
| Snowball primitives | `snow/consensus/snowball/` | `parameters.go`, `{binary,unary,nnary}_{slush,snowflake,snowball}.go`, `tree.go`, `flat.go`, `factory.go`, `consensus.go` |
| Snowman consensus | `snow/consensus/snowman/` | `consensus.go`, `topological.go`, `snowman_block.go`, `block.go`, `oracle_block.go`, `metrics.go` |
| Snowman polls | `snow/consensus/snowman/poll/` | `set.go`, `interfaces.go`, `early_term_traversal.go`, `prefix.go`, `graph.go` |
| Bootstrap frontier polls | `snow/consensus/snowman/bootstrapper/` | `poll.go`, `minority.go`, `majority.go`, `sampler.go` |
| Snowman engine | `snow/engine/snowman/` | `engine.go`, `voter.go`, `issuer.go`, `config.go`, `memory_block.go`, `metrics.go` |
| Bootstrap engine | `snow/engine/snowman/bootstrap/` | `bootstrapper.go`, `storage.go`, `interval/` |
| State sync engine | `snow/engine/snowman/syncer/` | `state_syncer.go`, `config.go` |
| Get-server | `snow/engine/snowman/getter/` | `getter.go` |
| Block VM contract | `snow/engine/snowman/block/` | `vm.go`, `state_syncable_vm.go`, `batched_vm.go` |
| Common engine | `snow/engine/common/` | `engine.go`, `sender.go`, `vm.go`, `no_ops_handlers.go`, `bootstrapable.go`, `state_syncer.go`, `tracker/` |
| Networking | `snow/networking/` | `router/chain_router.go`, `handler/handler.go`, `sender/sender.go`, `timeout/manager.go`, `benchlist/`, `tracker/` |
| Shared core | `snow/` | `context.go`, `acceptor.go`, `decidable.go`, `state.go`, `choices/`, `validators/`, `uptime/` |

---

## 3. The Snowball decision family

The whole family is built up in layers. Each layer adds one idea on top of the previous.

### 3.1 Parameters

`snowball.Parameters` (`snow/consensus/snowball/parameters.go:53`) governs every snow instance:

| Field | Default | Meaning |
|-------|---------|---------|
| `K` | 20 | nodes sampled per poll |
| `AlphaPreference` | 15 | votes needed to **change preference** (must be `> K/2`) |
| `AlphaConfidence` | 15 | votes needed to **increment a confidence counter** (`AlphaPreference ≤ AlphaConfidence ≤ K`) |
| `Beta` | 20 | consecutive confidence-incrementing polls required to **finalize** |
| `ConcurrentRepolls` | 4 | target number of outstanding polls while work remains (`0 < … ≤ Beta`) |
| `OptimalProcessing` | 10 | soft cap on processing blocks before throttling block builds |
| `MaxOutstandingItems` | 256 | health threshold on processing items |
| `MaxItemProcessingTime` | 30s | health threshold on per-item latency |

`Verify()` (`parameters.go:93`) enforces `K/2 < AlphaPreference ≤ AlphaConfidence ≤ K` and the repoll/processing bounds. `MinPercentConnectedHealthy()` (`parameters.go:118`) derives the required connected-stake fraction (`AlphaConfidence/K` plus a 0.2 buffer) used by health checks and gating queries.

### 3.2 Slush → Snowflake → Snowball (binary)

- **Slush** (`binary_slush.go`): the bare minimum — just remembers the last winning color (`preference`). `RecordSuccessfulPoll(choice)` sets the preference.
- **Snowflake** (`binary_snowflake.go:25`): adds **confidence counters**. `RecordPoll(count, choice)` (`binary_snowflake.go:47`):
  - if `count < alphaPreference` → `RecordUnsuccessfulPoll()` clears all confidence (`clear(sf.confidence)`).
  - if the winning `choice` differs from the current preference → confidence is cleared *before* recording.
  - then it walks `terminationConditions` (ascending `alphaConfidence`): for each threshold met it increments `confidence[i]`; once `confidence[i] >= beta` it sets `finalized = true`. Thresholds not met clear the tail of the counters. (The list supports multiple `(alphaConfidence, beta)` tiers; the standard factories build a single tier via `newSingleTerminationCondition` — `parameters.go:131`.)
- **Snowball** (`binary_snowball.go:18`): wraps Snowflake and adds **`preferenceStrength`** — a *cumulative* tally of how many polls preferred each choice. `Preference()` returns the strongest cumulative choice (lazy tie-break) rather than the most recent one, making preference sticky against transient flips. Once Snowflake finalizes, `Preference()` defers to the finalized Snowflake choice (`binary_snowball.go:31`).

### 3.3 Unary and N-ary

- **Unary** (`unary_snowflake.go`, `unary_snowball.go`): a single-choice instance, used while a block has no competing sibling. `Extend(choice)` (`unary_snowball.go:35`) converts a unary instance into a binary one (cloning the confidence/strength state) the moment a conflict appears. `Clone()` duplicates state for tree splits.
- **N-ary** (`nnary_*.go`): the same Slush/Snowflake/Snowball stack but over an unbounded set of `ids.ID` choices, with `preferenceStrength` as a `map[ids.ID]int`. `Nnary.Add(id)` introduces new choices.

`Factory` (`consensus.go:44`) abstracts construction: `SnowballFactory` and `SnowflakeFactory` (`factory.go`) produce Snowball (with preference strength) or plain Snowflake instances.

### 3.4 The Tree — deciding among conflicting IDs

`Tree` (`tree.go:36`) implements the `snowball.Consensus` interface (`consensus.go:15`) as a **modified patricia trie keyed on the bits of the candidate IDs**. Each interior node is a unary Snowflake over a shared bit prefix; at a bifurcation (two IDs whose prefixes diverge at bit *i*) the unary instance is `Extend`ed into a binary Snowball that decides which subtree wins. This means a single set of votes decides every prefix simultaneously, and `RecordPoll(votes)` (`tree.go:67`) filters out votes inconsistent with the decided prefix, then pushes the remaining votes down the tree. `shouldReset` (`tree.go:53`) defers `RecordUnsuccessfulPoll` traversals lazily: if a node fails to reach alpha, none of its descendants can either, so the reset is flagged and applied on the next traversal.

`Flat` (`flat.go`) is the trivial alternative: a single n-ary instance whose `RecordPoll` just takes the bag's mode.

---

## 4. Snowman — linear-chain consensus

`snowman.Consensus` (`snow/consensus/snowman/consensus.go:19`) is the interface the engine programs against:

```go
Initialize(ctx *snow.ConsensusContext, params snowball.Parameters, lastAcceptedID ids.ID, lastAcceptedHeight uint64, lastAcceptedTime time.Time) error
Add(Block) error
Processing(ids.ID) bool
IsPreferred(ids.ID) bool
LastAccepted() (ids.ID, uint64)
Preference() ids.ID
PreferenceAtHeight(height uint64) (ids.ID, bool)
RecordPoll(context.Context, bag.Bag[ids.ID]) error
GetParent(id ids.ID) (ids.ID, bool)
NumProcessing() int
```

`snowman.Block` (`block.go:24`) embeds `snow.Decidable` (`ID/Accept/Reject`) and adds `Parent()`, `Verify(ctx)`, `Bytes()`, `Height()`, `Timestamp()`. An `OracleBlock` (`oracle_block.go`) exposes two pre-ordered option children (used by the P-Chain for proposal/commit/abort).

### 4.1 Topological — the implementation

`Topological` (`topological.go:46`) is the only production `Consensus`. Key state:

- `blocks map[ids.ID]*snowmanBlock` — last accepted block + all processing blocks. A `snowmanBlock` (`snowman_block.go:12`) holds its `Block`, a `snowball.Consensus` instance (`sb`) that decides *which child is canonical*, the `children` map, and a `shouldFalter` flag.
- `preferredIDs` / `preferredHeights` / `preference` — the strongly-preferred branch.

**The central idea:** a chain of blocks is a chain of single-child decisions. Each parent owns a `snowball.NewTree` instance that decides among its issued children (`snowman_block.go:38`). One network poll over leaf-block IDs is transitively applied to *every ancestor* on the way to the genesis, so a single round advances confidence on the whole preferred branch.

**`Add(blk)`** (`topological.go:147`): rejects duplicates, requires the parent to be tracked, calls `parentNode.AddChild`, and extends the preferred branch if the parent was the tip.

**`RecordPoll(ctx, voteBag)`** (`topological.go:246`) is the heart of the algorithm:

1. `pollNumber++`.
2. If `voteBag.Len() ≥ AlphaPreference`, run `calculateInDegree` (`topological.go:362`) — a Kahn topological-sort setup that, for each voted block, walks to the genesis recording in-degrees and bucketing votes onto each block's *parent* (the node that owns the deciding Snowball). Then `pushVotes` (`topological.go:423`) drains leaves, transitively summing child votes into parents, emitting a `voteStack` of `(parentID, votes)` entries that each reached ≥ alpha.
3. `vote(ctx, voteStack)` (`topological.go:469`) pops the stack from genesis outward, applying transitive **falters** (`RecordUnsuccessfulPoll` on branches that did *not* get alpha), calling `parentBlock.sb.RecordPoll(votes)`, and — when the **last-accepted block's** Snowball finalizes — calling `acceptPreferredChild`. Siblings of the new preference are flagged `shouldFalter` so their confidence resets before any future positive vote.
4. `acceptPreferredChild` (`topological.go:599`) fires `ctx.BlockAcceptor.Accept(...)` **before** `child.Accept(ctx)` (honoring the Acceptor invariant), advances `lastAcceptedID/Height`, then rejects all conflicting siblings and `rejectTransitively` (`topological.go:671`) all their descendants.
5. The preferred branch is recomputed from the new preference down to its preferred leaf.

If fewer than alpha votes arrive (or the stack is empty), the whole tree is flagged to falter and `metrics.FailedPoll()` is recorded — confidence resets but the preferred branch is unchanged.

`HealthCheck` (`topological.go:316`) reports unhealthy when processing-block count exceeds `MaxOutstandingItems`, the oldest block exceeds `MaxItemProcessingTime`, or average acceptance time exceeds `maxAcceptanceTime` (15s).

---

## 5. The Snowman engine — turning network votes into decisions

`snow/engine/snowman/engine.go:49` defines `Engine`, which implements `common.Engine` (`snow/engine/common/engine.go:23`). It embeds no-op handlers for messages it ignores (state-summary frontier, accepted, ancestors, simplex), delegating App messages and `Connected/Disconnected` to the VM.

### 5.1 Config & construction

`Config` (`snow/engine/snowman/config.go:17`) wires: `Ctx *snow.ConsensusContext`, `VM block.ChainVM`, `Sender common.Sender`, `Validators validators.Manager`, `ConnectedValidators tracker.Peers`, `Params snowball.Parameters`, `Consensus snowman.Consensus`, `PartialSync`, and `common.AllGetsServer` (the getter). `New` (`engine.go:98`) builds the early-termination poll factory via `poll.NewEarlyTermFactory(AlphaPreference, AlphaConfidence, reg, config.Consensus)` and a `poll.Set`.

### 5.2 Inbound message handlers (the loop)

- **`PushQuery` / `PullQuery`** (`engine.go:320` / `engine.go:306`): immediately `sendChits` with this node's preference, then issue the queried block (parsing/fetching ancestors as needed).
- **`Put`** (`engine.go:209`) / **`GetFailed`** (`engine.go:280`): responses to our `Get` requests for missing ancestors. `Put` validates the returned block matches the outstanding `blkReqs` entry (`bimap.BiMap[common.Request, ids.ID]`).
- **`Chits`** (`engine.go:361`): the response to our query. It records the peer's accepted frontier (`acceptedFrontiers.SetLastAccepted`), issues the referenced `preferredID`/`preferredIDAtHeight` blocks, then schedules a **`voter`** job that waits until those blocks are issued before applying the vote. `QueryFailed` (`engine.go:419`) substitutes the peer's last-known accepted block (or schedules an empty-vote `voter`).

### 5.3 Issuance pipeline

A block can't enter consensus until its parent has. The engine uses a `job.Scheduler[ids.ID]` (`blocked`) so blocks/votes block on dependencies:

- `issueFrom` / `issueFromByID` / `issueWithAncestors` (`engine.go:709`–`795`) walk the ancestry, calling `issue` per block and `sendRequest` (a `Get`) for any missing ancestor.
- `issue` (`engine.go:800`) enqueues an **`issuer`** job depending on the parent ID.
- `issuer.Execute` (`snow/engine/snowman/issuer.go:27`): if the parent was abandoned, abandon this block; otherwise call `deliver`.
- `deliver` (`engine.go:944`) verifies the block (`addUnverifiedBlockToConsensus` → `blk.Verify` → `Consensus.Add`), handles oracle option children, sets the VM preference, and — if the block is now preferred — fires a query via `sendQuery`. Verification failures route the block into the `unverifiedBlockCache` / `unverifiedIDToAncestor` tree so that votes for it can still **bubble** up to a valid ancestor.

### 5.4 Voting & repolling

`voter.Execute` (`voter.go:28`):
1. **Bubble** each response option to its nearest processing ancestor via `getProcessingAncestor` (`engine.go:1125`) — votes for not-yet-verified or already-decided blocks are redirected or dropped.
2. `polls.Vote(requestID, nodeID, vote)` (or `Drop`) returns finished poll results (a slice because finishing one poll may unblock older ones — `poll/set.go:141`).
3. For each result, `Consensus.RecordPoll(ctx, result)` applies the vote; then `VM.SetPreference`.
4. If nothing is processing, the engine **quiesces**; otherwise `repoll` issues up to `ConcurrentRepolls` new pull queries (`engine.go:696`).

`sendQuery` (`engine.go:871`) samples `K` validators via `Validators.Sample(SubnetID, K)`, registers a poll (`polls.Add`), and sends a `PushQuery` (with block bytes) or `PullQuery` (ID only). It first calls `abortDueToInsufficientConnectedStake` (`engine.go:926`) — queries are dropped if connected stake `< AlphaConfidence/K`.

### 5.5 Early-termination polls

`earlyTermPoll` (`poll/early_term_traversal.go:132`) finishes a poll *before* all K responses arrive when the outcome is already determined: (1) no outstanding votes; (2) impossible to reach `alphaPreference`; (3) a value reached `alphaPreference` but can't reach `alphaConfidence`; (4) a value reached `alphaConfidence`. Crucially it accounts for **transitive votes**: it builds a vote graph (`buildVoteGraph`), sums child votes into parents, and computes shared-prefix vote counts (`prefix.go`) so that votes for descendant blocks count toward ancestor and prefix Snowflake instances — mirroring the Snowman tree's transitive voting. Termination reasons are tracked as metrics (`exhausted` / `early_fail` / `early_alpha`).

### 5.6 Block building & gossip

`Notify(PendingTxs)` increments `pendingBuildBlocks`; `buildBlocks` (`engine.go:649`) calls `VM.BuildBlock` while `NumProcessing() < OptimalProcessing`, issuing the result on top of the current preference. `Gossip` (`engine.go:158`) sends a single uniform-sampled pull query for the next height to propagate the preferred branch.

---

## 6. Reaching the tip: state sync → bootstrap → normal op

A chain advances through engine states `Initializing → StateSyncing → Bootstrapping → NormalOp` (`snow/state.go:12`). The `Handler` selects the starting gear (`handler.go:221`): state-syncer if the VM supports it and it's enabled, else bootstrapper.

### 6.1 State sync (`syncer/state_syncer.go`)

`stateSyncer` implements `common.StateSyncer`. Flow:
1. **Frontier:** sample `K` `StateSyncBeacons`, send `GetStateSummaryFrontier` to seeders (`startup` — `state_syncer.go:470`), collect summaries.
2. **Vote:** once an alpha-weighted fraction of seeders reply, ask *all* beacons to filter the summary heights (`GetAcceptedStateSummary`); accumulate per-summary weight (`AcceptedStateSummary` — `state_syncer.go:267`).
3. Drop summaries below `Alpha` weight; pick the highest (or resume a locally-available one). `summary.Accept(ctx)` returns a `StateSyncMode` (`Skipped` / `Static` / `Dynamic`).
4. On `Static`, the engine waits for the VM's `StateSyncDone` notification; on `Dynamic`, it proceeds to bootstrapping while the VM syncs in the background; otherwise it proceeds directly. Insufficient responses → `startup` retry.

### 6.2 Bootstrapping (`bootstrap/bootstrapper.go`)

`Bootstrapper` implements `common.BootstrapableEngine`. The protocol (documented at `bootstrapper.go:56`):

1. Wait for sufficient connected stake (`StartupTracker.ShouldStart`).
2. **Frontier poll:** sample `SampleK` beacons (`bootstrapper.Sample`), build a `bootstrapper.Minority` poll, send `GetAcceptedFrontier`.
3. **Majority poll:** send the frontier set via `GetAccepted` to *all* beacons; a block accepted by majority weight is taken as network-accepted (`bootstrapper.Majority` — `snow/consensus/snowman/bootstrapper/majority.go`). Both polls implement `bootstrapper.Poll` (`poll.go:13`: `GetPeers / RecordOpinion / Result`).
4. **Sync ancestry:** `startSyncing` (`bootstrapper.go:389`) collects missing block IDs; `fetch` sends `GetAncestors` to a `PeerTracker`-selected peer; `Ancestors` (`bootstrapper.go:467`) batch-parses and `process`es them into an on-disk `interval.Tree`, discovering further missing parents.
5. **Execute:** when no IDs are missing, `tryStartExecuting` (`bootstrapper.go:668`) executes the buffered blocks (`parseAcceptor`).
6. **Restart-until-converged:** if blocks executed this round dropped below half the previous round, restart bootstrapping to catch the moving tip (`bootstrapper.go:724`); otherwise notify the `BootstrapTracker`. If other chains in the subnet are still syncing, re-bootstrap after a 10s timeout (`TimeoutRegistrar.RegisterTimeout`); else call `onFinished`, which transitions the chain to the Snowman engine's `Start`.

`Engine.Start` (`engine.go:466`) then initializes `Consensus`, sets the VM preference (handling oracle last-accepted), sets state to `NormalOp`, and notifies the VM.

### 6.3 The getter (`getter/getter.go`)

`getter` implements `common.AllGetsServer` — the *serving* side of every Get request, available in **all** states. It answers `GetAcceptedFrontier`, `GetAccepted`, `GetAncestors` (bounded by time/size), `Get` (→ `Put`), and the state-summary gets, all by reading the VM. It never participates in voting.

---

## 7. Networking glue

### 7.1 Handler (`networking/handler/handler.go`)

The `Handler` (`handler.go:53`) owns the **chain context lock** and converts router-delivered messages into engine calls. It runs three dispatch goroutines (`handler.go:244`):

- **`dispatchSync`** — pops from `syncMessageQueue`, takes `ctx.Lock`, and calls `handleSyncMsg` (`handler.go:438`), a large type switch over consensus/bootstrap/state-sync ops that selects the right engine via `engineManager.Get(engineType).Get(state)`. Errors are **fatal** (chain shuts down). Response messages are never silently dropped here — if a field is malformed the handler synthesizes the corresponding `…Failed` call.
- **`dispatchAsync`** — App messages (`AppRequest/Response/Gossip/Error`) run on a bounded `errgroup` worker pool *without* the chain lock (`handler.go:384`, `executeAsyncMsg`).
- **`dispatchChans`** — VM notifications (`msgFromVMChan`) and the internal gossip ticker (`handler.go:405`).

`Push` (`handler.go:307`) routes App ops to the async queue and everything else to the sync queue. `popUnexpiredMsg` (`handler.go:948`) drops messages past their deadline. `ShouldHandle` (`handler.go:204`) enforces subnet allow-listing. The handler also reports per-message CPU/disk usage to the `ResourceTracker`.

### 7.2 ChainRouter (`networking/router/chain_router.go`)

`ChainRouter` (`chain_router.go:67`) routes inbound P2P messages to the correct `handler.Handler` by `ChainID` and owns the **request/response lifecycle**:

- **`RegisterRequest`** (`chain_router.go:152`): called by the sender for each outbound request. It records a `requestEntry` keyed by `ids.RequestID{NodeID, ChainID, RequestID, Op}` in `timedRequests` and registers an adaptive timeout with `timeoutManager.RegisterRequest`. If no response arrives, the timeout fires `handleMessage(timeoutMsg, internal=true, timeout=true)`, delivering a synthetic `…Failed` to the engine. Latency is *not* measured for self-messages or `Put` (an adversary could skew it).
- **`handleMessage`** (`chain_router.go:231`): validates ChainID, chain existence, and `ShouldHandle`; drops unrequested messages while the chain is `Executing`; for responses it matches the outstanding request (`clearRequest`), feeds the measured latency to `timeoutManager.RegisterResponse`, then `chain.Push`es to the handler. `FailedToResponseOps` maps failure ops to their expected response so duplicates/benched-early-failures are handled exactly once (`req.handled`).
- **`Connected/Disconnected/Benched/Unbenched`**: turn peer/bench events into internal `Connected`/`Disconnected` messages pushed to the relevant chains. **Invariant:** a node benched on *any* chain is treated as disconnected on *all* chains. Implements `benchlist.Benchable`.

### 7.3 Sender (`networking/sender/sender.go`)

`sender` (`sender.go:37`) implements `common.Sender` (`snow/engine/common/sender.go:49`) — the engine's outbound API (`SendPullQuery`, `SendPushQuery`, `SendGet`, `SendChits`, `SendGetAncestors`, `SendGetAcceptedFrontier`, state-sync sends, etc.). It is a thin wrapper over the `ExternalSender` (the network) that **registers a timeout with the router before sending** (start the timer on the send side) so the engine reliably receives either a real response or a synthetic failure. If the timeout manager reports a peer benched, the request fails immediately (counted in `failedDueToBench`).

### 7.4 Timeout manager & benchlist

`timeout.Manager` (`networking/timeout/manager.go:47`) wraps a `timer.AdaptiveTimeoutManager`: timeout duration adapts to observed network latency (`TimeoutDuration`). `RegisterRequest` prepends a benchlist `RegisterFailure` for non-App timeouts; `RegisterResponse` records latency and a benchlist `RegisterResponse`.

`benchlist` (`networking/benchlist/benchlist.go:82`) projects whether a node is likely to time out using an EWMA failure probability (`DefaultHalflife` 1m). A node projected to fail is **benched** (`DefaultBenchProbability` .5) for `DefaultBenchDuration` (5m), during which requests to it fail instantly. All mutable state is owned by a single consumer goroutine fed by a non-blocking event queue; `IsBenched` reads a published snapshot. `MaxPortion` caps total benched stake.

---

## 8. Shared core

### 8.1 ConsensusContext (`snow/context.go`)

`snow.Context` (`context.go:33`) carries `NetworkID/SubnetID/ChainID/NodeID`, the chain logger, the **deprecated coarse `Lock`** (the chain context lock taken by the handler), `SharedMemory`, `ValidatorState` (the P-Chain validator lookup), `WarpSigner`, and `ChainDataDir`. `ConsensusContext` (`context.go:70`) adds the metrics `Registerer`, the `BlockAcceptor`/`TxAcceptor`/`VertexAcceptor` callbacks, and atomics `State` (`EngineState`), `Executing`, and `StateSyncing`.

### 8.2 Decidable, Acceptor, Status

- `snow.Decidable` (`decidable.go:16`): `ID()`, `Accept(ctx)`, `Reject(ctx)` — the base for blocks/txs/vertices.
- `snow.Acceptor` / `AcceptorGroup` (`acceptor.go:23`): `Accept(ctx, containerID, container)` **must be called before the container is committed as accepted** by the VM. The group fans out to registered acceptors per chain; a `dieOnError` acceptor failing shuts the chain down. This is how indexers and cross-chain shared memory observe acceptance.
- `choices.Status` (`choices/status.go:14`): `Unknown / Processing / Rejected / Accepted`.

### 8.3 Validators (`snow/validators`)

`validators.Manager` (`validators/manager.go:40`) holds the validator set **per subnet**: `AddStaker/AddWeight/RemoveWeight`, `GetWeight`, `Sample(subnetID, size)` (the K-sampling the engine uses), `TotalWeight/SubsetWeight`, and `RegisterCallbackListener`. The set is **sourced from the P-Chain**: `validators.State` (`validators/state.go:30`) exposes `GetCurrentHeight`, `GetValidatorSet(height, subnetID)`, and `GetCurrentValidatorSet(subnetID)`; the platformvm implements this and feeds the manager so that consensus sampling reflects on-chain stake. See [PlatformVM](platformvm.md).

### 8.4 Uptime (`snow/uptime`)

`uptime.Manager` (`uptime/manager.go:22`) = `Tracker` (`StartTracking/StopTracking`, `Connect/Disconnect`) + `Calculator` (`CalculateUptime`, `CalculateUptimePercent[From]`). It records per-node connected durations against persisted `State`, providing the uptime percentages the P-Chain uses for staking-reward eligibility.

---

## 9. Lifecycle & data flow

```
                inbound P2P message
                        │
                        ▼
              ┌──────────────────┐    RegisterRequest / RegisterResponse
              │   ChainRouter    │◄───────────────┐ (timeouts, latency, benchlist)
              │  route by ChainID│                │
              └────────┬─────────┘         ┌───────┴────────┐
              chain.Push(msg)              │ timeout.Manager│
                        │                  │  + benchlist   │
                        ▼                  └───────┬────────┘
              ┌──────────────────┐                 ▲
              │     Handler      │   sync queue ───┘ │ Sender.SendX → ExternalSender (network)
              │ sync/async/chan  │   async queue     │
              │  (holds ctx.Lock)│   VM/gossip chan   │
              └────────┬─────────┘                    │
        handleSyncMsg → engine method                 │
                        │                              │
                        ▼                              │
   ┌────────────────────────────────────────────┐     │
   │  Engine (StateSyncer→Bootstrapper→Snowman)  │─────┘ queries / gets / chits
   │                                              │
   │  PushQuery/PullQuery → issue block → deliver │
   │  Chits → voter → polls.Vote                  │
   │            │                                 │
   │            ▼                                 │
   │   Consensus.RecordPoll(votes)                │
   │     (Topological tree of Snowball)           │
   │        │              │                      │
   │        ▼              ▼                      │
   │  BlockAcceptor.Accept  blk.Accept/Reject     │
   │        │              │                      │
   │        ▼              ▼                      │
   │   AcceptorGroup      VM (block.ChainVM)      │
   └────────────────────────────────────────────┘
```

---

## 10. Component boundaries & relationships

| Boundary | Upstream (caller) | Downstream (callee) | Interface |
|----------|-------------------|---------------------|-----------|
| Network → Router | `network` package (transport) | `ChainRouter` | `router.ExternalHandler.HandleInbound` (`chain_router.go:215`) |
| Router → Handler | `ChainRouter` | `handler.Handler` | `Handler.Push` (`handler.go:307`) |
| Handler → Engine | `handler` | `common.Engine` | `common.Handler`/`common.Engine` (`engine.go:23`) |
| Engine → Consensus | `snowman.Engine` | `snowman.Topological` | `snowman.Consensus` (`consensus.go:19`) |
| Consensus → snow instances | `Topological` | `snowball.Tree` | `snowball.Consensus` (`snowball/consensus.go:15`) |
| Engine → poll mgmt | `snowman.Engine` | `poll.Set` / `earlyTermPoll` | `poll.Set` (`poll/interfaces.go:15`) |
| Engine → VM | `snowman.Engine`, bootstrap, getter | the VM | `block.ChainVM` / `block.StateSyncableVM` (see [VM Framework](vm-framework.md)) |
| Engine → Sender | engine/bootstrap/syncer | `networking/sender` | `common.Sender` (`common/sender.go:49`) |
| Sender → Router/Network | `sender` | `ChainRouter`, `ExternalSender` | `router.InternalHandler.RegisterRequest`, `ExternalSender` |
| Router → Timeout/Bench | `ChainRouter` | `timeout.Manager`, `benchlist` | `Manager.RegisterRequest/Response`, `benchlist.Benchable` |
| Consensus → Acceptors | `Topological.acceptPreferredChild` | `AcceptorGroup` | `snow.Acceptor.Accept` (`acceptor.go:23`) — **called before VM commit** |
| Engine → Validators | engine, bootstrap, syncer | `validators.Manager` | `Manager.Sample/GetWeight/TotalWeight` (`validators/manager.go:40`) |
| Validators ← P-Chain | platformvm | `validators.Manager` | `validators.State` (`validators/state.go:30`) |

---

## 11. Key behaviors, invariants, concurrency

- **Topological order of decisions** (`block.go:14`): `Verify` is called only after the parent verified; `Accept`/`Reject` only after the parent is decided; the genesis/last-accepted block is implicitly accepted.
- **Acceptor-before-commit invariant** (`topological.go:607`): `ctx.BlockAcceptor.Accept` runs *before* `child.Accept`, so external observers never miss an acceptance the VM has already committed.
- **Vote bubbling**: votes for unverified or pruned blocks are redirected to the nearest processing ancestor (`getProcessingAncestor`) or dropped — a Byzantine peer cannot stall consensus by voting for unknown blocks.
- **Finalization safety**: a block is accepted only when its parent's Snowball instance is `Finalized()` *and* its parent is the current last-accepted block (`topological.go:524`).
- **Concurrency model**: all sync consensus work runs under the single per-chain `ctx.Lock` held by the Handler's sync dispatcher; App messages run lock-free on the async pool. The router has its own lock and must never be called while holding a chain lock except for `RegisterResponse` (`chain_router.go:74`). Benchlist mutable state is single-goroutine-owned with a non-blocking producer queue.
- **Quiescence**: when `Consensus.NumProcessing() == 0` the engine stops repolling (`voter.go:66`), saving bandwidth when idle.
- **Connected-stake gating**: queries abort when connected stake `< AlphaConfidence/K` (`engine.go:926`); health requires `MinPercentConnectedHealthy`.
- **Error handling**: any error returned from a sync/chan handler is treated as fatal and shuts the chain down (`handler.go:372`); async handlers likewise (`handler.go:770`). Bootstrap/state-sync out-of-order responses are dropped by `requestID` mismatch.
- **Bootstrap termination**: the restart-until-converged rule (`numToExecute < previouslyExecuted/2`, `bootstrapper.go:724`) guarantees the loop ends even as new blocks are produced.

---

## 12. Configuration / parameters

- **Snowball**: `K`, `AlphaPreference`, `AlphaConfidence`, `Beta`, `ConcurrentRepolls`, `OptimalProcessing`, `MaxOutstandingItems`, `MaxItemProcessingTime` (`parameters.go:38`; defaults in §3.1). Validated by `Parameters.Verify()`.
- **Engine**: `PartialSync` (vote only for widely-preferred blocks; `engine.go:584`), `nonVerifiedCacheSize` 64 MiB.
- **Bootstrap**: `SampleK`, `AncestorsMaxContainersReceived`, `bootstrappingDelay` 10s, `maxOutstandingBroadcastRequests` 50 (`bootstrapper.go:32`).
- **Benchlist**: `Halflife` 1m, `BenchProbability` .5, `UnbenchProbability` .2, `BenchDuration` 5m, `MaxPortion` (`benchlist.go:27`).
- **Timeouts**: adaptive via `timer.AdaptiveTimeoutConfig`.

---

## 13. Cross-references

- [Overview](overview.md) — system-wide context and the per-chain pipeline.
- [Simplex](simplex.md) — the alternative BFT consensus engine (`simplex/`, `snow/consensus/simplex/`).
- [Networking](networking.md) — the P2P transport, `ExternalSender`, peer management beneath the router/sender.
- [VM Framework](vm-framework.md) — `block.ChainVM`, `StateSyncableVM`, and how the engine drives the VM.
- [PlatformVM](platformvm.md) — source of the validator sets consumed by `validators.State`/`Manager` and consumer of `uptime`.
- [Chains](chains.md) — the `chains.Manager` that constructs handlers, engines, senders, and registers chains with the router.
- [Primitives](primitives.md) — `ids`, `bag.Bag`, `set.Set`, `linked.Hashmap`, `bimap.BiMap` used throughout.
```
