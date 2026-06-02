# Chains, Genesis, Node Initialization, Configuration, and Upgrades

## 1. Chain Manager (`chains/`)

### 1.1 Role

The chain manager owns the full lifecycle of every blockchain running on the node:
- Creates chains (from P-Chain `CreateChainTx` or direct genesis).
- Associates VMs with chains.
- Registers chains with the consensus router.
- Provides lookup by chain ID or alias.

### 1.2 Manager Interface

```go
// chains/manager.go
type Manager interface {
    ids.Aliaser                   // alias ↔ chain ID resolution
    ids.AliasReader

    QueueChainCreation(ChainParameters)
    SubnetID(chainID ids.ID) (ids.ID, error)
    IsBootstrapped(chainID ids.ID) bool
    Subnets() *Subnets
}
```

```go
type ChainParameters struct {
    ID           ids.ID         // unique chain ID
    SubnetID     ids.ID         // subnet that validates this chain
    GenesisData  []byte         // genesis bytes
    VMID         ids.ID         // VM to use
    FxIDs        []ids.ID       // feature extensions
    CustomBeacons validators.Manager  // only for P-chain
}
```

### 1.3 Chain Creation Flow

```
P-Chain accepts CreateChainTx
  → P-Chain calls chainManager.QueueChainCreation(params)
     → Appended to chainsQueue (unbounded deque)
     
Once P-Chain bootstraps:
  → unblockChainCreatorCh signal sent
  → dispatchChainCreator() goroutine wakes
  → Processes queue one by one
  → createChain(params)
     1. Get VM factory from VMManager
     2. Create DB prefixes (vm, vertex, bootstrapping)
     3. Raw VM = vmFactory.New()
     4. Optionally wrap: TracedVM → ProposerVM → MeterVM
     5. vm.Initialize(genesis, config, fxs, appSender)
     6. Create consensus engine (Snowman or Avalanche)
     7. Create handler (message dispatcher)
     8. Register with router
     9. Start handler goroutines
```

Failure handling: For **critical chains** (P, X, C), any error triggers node shutdown.

### 1.4 VM Wrapping Stack

For Snowman (linear) chains:
```
User VM
  ↓ TracedVM (if tracing enabled)
  ↓ ProposerVM
  ↓ MeterVM (if metering enabled)
  ↓ TracedVM (second layer)
```

For Avalanche (DAG) chains (X-Chain before linearization):
```
User VM (LinearizableVM)
  ↓ Dual-engine: Avalanche DAG engine + Snowman linearizer
```

### 1.5 Registrant Pattern

After each chain is created, `notifyRegistrants()` fires. Registrants implement:
```go
type Registrant interface {
    RegisterChain(name string, ctx *snow.ConsensusContext, vm common.VM)
}
```

This is how API endpoints get registered: the API server is a registrant that calls `vm.CreateHandlers()` and mounts them.

### 1.6 Subnet Management (`chains/subnets.go`)

```go
type Subnets struct {
    subnetLock    sync.RWMutex
    subnetConfigs map[ids.ID]subnets.Config
    subnets       map[ids.ID]subnets.Subnet  // subnet state tracking
}
```

`GetOrCreate(subnetID)` creates a subnet on first access with default Snowball config. Each subnet tracks bootstrap state: a chain is considered bootstrapped when its consensus engine transitions to `NormalOp`.

---

## 2. Atomic Memory (`chains/atomic/`)

Cross-chain transfers use atomic shared memory — a bidirectional, lock-protected key-value store pair for each pair of chains.

### 2.1 Architecture

```go
type Memory struct {
    lock  sync.Mutex
    locks map[ids.ID]*rcLock  // reference-counted lock per shared pair
    db    database.Database
}
```

For chains A and B:
- **Shared pair ID** = `Hash(Sort([A, B]))` — symmetric regardless of order.
- Under the shared pair, there are two namespaces: `inbound` and `outbound`, relative to each chain's perspective.

```
db layout:
  [sharedID] /
    [chainA_perspective] /
      inbound/   ← data sent from B to A
      outbound/  ← data sent from A to B
```

### 2.2 SharedMemory Interface

```go
type SharedMemory interface {
    Get(peerChainID ids.ID, keys [][]byte) (values [][]byte, err error)

    Indexed(
        peerChainID ids.ID,
        traits [][]byte,
        startTrait, startKey []byte,
        limit int,
    ) (values [][]byte, lastTrait, lastKey []byte, err error)

    Apply(requests map[ids.ID]*Requests, batches ...database.Batch) error
}
```

`Apply` is atomic: applies all requests from multiple chains in a single database batch. Lock ordering (sorted shared IDs) prevents deadlocks.

### 2.3 Element Structure

```go
type Element struct {
    Key    []byte
    Value  []byte
    Traits [][]byte  // indexed traits for `Indexed` lookup
}

type Requests struct {
    RemoveRequests [][]byte
    PutRequests    []*Element
}
```

### 2.4 Cross-Chain Transfer Flow

```
Chain A (e.g., X-Chain) runs ExportTx:
  → avax.Apply({ChainB: Requests{PutRequests: [utxo]}})
  → Writes to A's outbound / B's inbound under sharedID(A,B)

Chain B (e.g., P-Chain) runs ImportTx:
  → SharedMemory.Get(ChainA, keys)          ← reads from inbound
  → After import accepted: Apply({ChainA: Requests{RemoveRequests: [keys]}})
  → Removes the consumed UTXOs
```

Both operations are committed atomically with the accepting block's database writes (both are in the same `database.Batch`).

---

## 3. Genesis (`genesis/`)

### 3.1 Config Structure

```go
type Config struct {
    NetworkID                  uint32
    Allocations                []Allocation    // address → AVAX distributions
    StartTime                  uint64          // network start (Unix)
    InitialStakeDuration       uint64          // validator duration (seconds)
    InitialStakeDurationOffset uint64          // stagger between validators
    InitialStakedFunds         []ids.ShortID   // addresses with locked stakes
    InitialStakers             []Staker        // genesis validators
    CChainGenesis              string          // serialized C-Chain genesis
    Message                    string          // network message
}

type Allocation struct {
    ETHAddr        ids.ShortID    // Ethereum address for X-chain claiming
    AVAXAddr       ids.ShortID    // Avalanche address
    InitialAmount  uint64         // unlocked on X-chain
    UnlockSchedule []LockedAmount // locked tranches on P-chain
}

type Staker struct {
    NodeID        ids.NodeID
    RewardAddress ids.ShortID
    DelegationFee uint32
    Signer        *signer.ProofOfPossession
}
```

### 3.2 Genesis Generation (`FromConfig`)

**Step 1: X-Chain Genesis**
- Creates AVAX asset (`CreateAssetTx`) as fixed-cap with `InitialAmount` distributed to all `Allocations`.
- `ETHAddr` stored as memo bytes for later cross-chain claiming.
- Result: X-chain genesis bytes, AVAX asset ID.

**Step 2: P-Chain Genesis**
- Creates `PermissionlessValidator` for each `Staker`.
- Distributes staked funds from `InitialStakedFunds` proportionally across validators.
- Stagger start times: validator `i` starts at `StartTime + i × InitialStakeDurationOffset`.
- Creates locked-unlock P-chain UTXOs per `Allocation.UnlockSchedule`.
- Creates two chains: X-Chain (VMID = AVM) and C-Chain (VMID = EVM).
- Embeds C-Chain genesis from `CChainGenesis` string.

**Step 3: Validation**
- All times monotonically ordered; start time not in future.
- Staked funds cover validator stakes.
- Supply > 0.

### 3.3 Network-Specific Configs

| Network | Config file | Special |
|---------|-------------|---------|
| Mainnet | `genesis_mainnet.json` | CortinaXChainStopVertexID set |
| Fuji | `genesis_fuji.json` | Earlier upgrade times |
| Local | `genesis_local.json` | All upgrades activated at genesis |

### 3.4 Bootstrap Nodes

Each network has a list of embedded bootstrap nodes (`bootstrappers_mainnet.json` etc.):
```go
type Bootstrapper struct {
    ID  ids.NodeID
    IP  netip.AddrPort
}
```

`SampleBootstrappers(networkID, count)` returns a random sample. Used during node startup to find initial peers before the validator set is known.

### 3.5 Known Validators

Embedded per-network JSON `validators.json`. Used to verify P-Chain transactions during bootstrap before the full validator set is available.

---

## 4. Node Initialization (`node/`, `app/`, `main/`)

### 4.1 Entry Point (`main/main.go`)

```
main()
  1. Register EVM extras: evm.RegisterAllLibEVMExtras()
  2. Parse flags via Cobra
  3. If --version: print and exit
  4. config.GetNodeConfig() → NodeConfig
  5. app.New(nodeConfig) → App
  6. os.Exit(app.Run(nodeApp))
```

### 4.2 Application Lifecycle (`app/app.go`)

```
app.New(config):
  1. Set directory permissions
  2. Create logging factory
  3. Raise file descriptor limit
  4. node.New(config) → Node
  5. Return App struct

app.Run(app):
  1. Start app (non-blocking)
  2. Register SIGINT, SIGTERM handlers → call app.Stop()
  3. Register SIGABRT handler → dump goroutine stacks
  4. Block on app.ExitCode()
  5. Return exit code
```

### 4.3 Node Initialization Sequence (`node/node.go`)

The `New(config)` function initializes in this order:

**Phase 1 — Identity:**
1. Extract `NodeID` from staking TLS certificate.
2. Load or generate BLS signer (ephemeral / key-file / RPC).
3. Generate `ProofOfPossession` (BLS signature proving key ownership).

**Phase 2 — Infrastructure:**
4. Create metrics gatherer (Prometheus multi-gatherer).
5. Detect NAT (UPnP/NAT-PMP).
6. Start API server (HTTP listener).
7. Initialize database (LevelDB or PebbleDB).
8. Create `atomic.Memory` (shared memory for cross-chain transfers).
9. Create `message.OutboundMsgBuilder`.

**Phase 3 — Networking:**
10. Create `validators.Manager`.
11. Initialize resource manager (CPU/disk tracking per peer).
12. Create P2P `network.Network` (TCP listener, TLS, peer management).
13. Create `snow.AcceptorGroup` (block/tx/vertex event bus).

**Phase 4 — Chain Ecosystem:**
14. Start health API.
15. Create `chains.Manager`.
16. Register built-in VMs: PlatformVM, AVM, EVM.
17. Scan plugin directory for external VMs.
18. Load chain/VM aliases.
19. Start admin and info APIs.
20. Create indexer (if enabled).
21. Start profiler (if enabled).

**Phase 5 — Chain Startup:**
22. Call `chainManager.StartChainCreator(platformChainParams)`.
   - P-Chain created synchronously.
   - Async goroutine waits for P-Chain bootstrap, then creates X and C chains.

### 4.4 Critical Chains

```go
CriticalChains = set.Set[ids.ID]{PChainID, XChainID, CChainID}
```

Failure to create any critical chain triggers `ShutdownNodeFunc(1)`.

### 4.5 Bootstrap Sequencing

```
Node connects to bootstrap peers
  → Validator list received from peers
  → Verified against embedded validators.json
  → Once ≥ 3/4 of bootstrap weight connected: unblock chain creator
  
P-Chain bootstraps:
  → Replays all P-Chain history
  → Rebuilds validator sets
  → Signals X/C chains to start
  
X-Chain bootstraps:
  → Replays all X-Chain history
  
C-Chain bootstraps:
  → Replays or state-syncs EVM history
```

---

## 5. Configuration (`config/`)

### 5.1 Flag Categories

**Identity:**
- `--data-dir` (default `~/.avalanchego`): All persistent data.
- `--genesis-file` / `--genesis-content`: Custom genesis (not Mainnet).
- `--upgrade-file`: Custom upgrade schedule.

**Network:**
- `--public-ip` / `--public-ip-resolution-service`: Public IP advertisement.
- `--listen-host:--listen-port` (default `0.0.0.0:9651`): P2P listener.
- `--bootstrap-ids`, `--bootstrap-ips`: Manual bootstrap peers.

**API:**
- `--http-host:--http-port` (default `127.0.0.1:9650`): HTTP API server.
- `--api-admin-enabled`: Enable admin API.
- `--api-health-enabled`: Enable health API (default true).
- `--api-metrics-enabled`: Enable metrics API (default true).

**Consensus (Snow):**
- `--snow-sample-size`: K (default 20).
- `--snow-preference-quorum-size`: AlphaPreference (default 15).
- `--snow-confidence-quorum-size`: AlphaConfidence (default 15).
- `--snow-commit-threshold`: Beta (default 20).
- `--snow-concurrent-repolls`: ConcurrentRepolls (default 4).

**Staking:**
- `--staking-tls-key-file`, `--staking-tls-cert-file`: Node TLS identity.
- `--staking-signer-key-file`: BLS private key file.
- `--staking-ephemeral-signer-enabled`: Generate ephemeral BLS key (dev only).

**Database:**
- `--db-type` (leveldb / pebbledb, default leveldb).
- `--db-dir` (default `~/.avalanchego/db`).

**Subnets:**
- `--track-subnets`: Comma-separated subnet IDs to validate.
- `--subnet-config-dir`: Per-subnet config files.

**Proposer VM:**
- `--proposer-min-block-delay` (default 100ms): Minimum time between blocks.

**Logging:**
- `--log-level` (verbo/debug/info/warn/error/fatal, default info).
- `--log-format` (auto/plain/json, default auto).

### 5.2 Config Loading Priority

```
hardcoded defaults
  ← config file (--config-file or --config-content)
  ← environment variables (AVALANCHE_ prefix)
  ← command-line flags
```

Later sources override earlier ones (Viper integration).

### 5.3 Chain Config

Per-chain config in `~/.avalanchego/configs/chains/<chainID>/config.json`:
```go
type ChainConfig struct {
    Config  []byte  // VM-specific configuration
    Upgrade []byte  // VM-specific upgrade bytes
}
```

Looked up by chain ID or alias (e.g., `C`, `X`, `P`).

### 5.4 Subnet Config

Per-subnet config in `~/.avalanchego/configs/subnets/<subnetID>.json`:
```go
type Config struct {
    // Snowball parameters (override defaults for this subnet)
    SnowParameters        snowball.Parameters
    // Simplex parameters (if using Simplex)
    SimplexParameters     simplex.Parameters
    // ProposerVM
    ProposerNumHistoricalBlocks uint64
}
```

---

## 6. Upgrade Management (`upgrade/`)

### 6.1 Upgrade Config

```go
type Config struct {
    // Apricot phases (2021)
    ApricotPhase1Time            time.Time
    ApricotPhase2Time            time.Time
    ApricotPhase3Time            time.Time
    ApricotPhase4Time            time.Time
    ApricotPhase4MinPChainHeight uint64
    ApricotPhase5Time            time.Time
    ApricotPhasePre6Time         time.Time
    ApricotPhase6Time            time.Time
    ApricotPhasePost6Time        time.Time

    // Banff (2022) — elastic subnets
    BanffTime time.Time

    // Cortina (2023) — X-Chain linearization
    CortinaTime              time.Time
    CortinaXChainStopVertexID ids.ID

    // Durango (2024)
    DurangoTime time.Time

    // Etna (2024) — L1 validators
    EtnaTime time.Time

    // Fortuna (2025)
    FortunaTime time.Time

    // Granite (2025) — Simplex, epochs
    GraniteTime          time.Time
    GraniteEpochDuration time.Duration  // 5 min mainnet, 30 sec local

    // Helicon (unscheduled)
    HeliconTime time.Time
}
```

### 6.2 Activation Predicates

Each upgrade exposes an `IsXxx(t time.Time) bool` method:
```go
func (c *Config) IsBanffActivated(t time.Time) bool {
    return !t.Before(c.BanffTime)
}
```

VMs and the engine check these predicates when processing blocks to apply the correct rules.

### 6.3 Special Cases

**ApricotPhase4:** Requires both time AND `pChainHeight ≥ ApricotPhase4MinPChainHeight`. Ensures validator set stability.

**CortinaXChainStopVertexID:** A well-known vertex ID that marks where the X-Chain DAG was linearized into a Snowman chain. All DAG vertices before this are treated as historical.

**GraniteEpochDuration:** Configurable per network. Controls Simplex epoch length (validator set snapshot interval).

### 6.4 Validation

```go
func (c *Config) Validate() error {
    // All upgrade times must be monotonically increasing
    // Returns error if any later upgrade precedes an earlier one
}
```

### 6.5 Network Activation Times

| Upgrade | Mainnet activation | Purpose |
|---------|-------------------|---------|
| ApricotPhase1 | March 2021 | Initial launch fixes |
| ApricotPhase2 | May 2021 | Fee burns |
| ApricotPhase3 | Sep 2021 | Fee structure |
| ApricotPhase4 | Oct 2021 | Validator set changes |
| ApricotPhase5 | Dec 2021 | Subnet functionality |
| Banff | Oct 2022 | Elastic subnets |
| Cortina | Apr 2023 | X-Chain linearization |
| Durango | Mar 2024 | Transfer subnet ownership, BaseTx |
| Etna | Nov 2024 | L1 validators, dynamic fees |
| Fortuna | 2025 | (details TBD) |
| Granite | 2025 | Simplex consensus, epochs |

---

## 7. VM Registry and Plugin System (`vms/`)

### 7.1 VM Manager

```go
type Manager struct {
    factories map[ids.ID]factory.Factory  // VMID → Factory
    aliases   map[ids.ID][]string         // VMID → aliases
}

type Factory interface {
    New(*snow.Context) (interface{}, error)
}
```

Built-in VMs registered at node startup:
- `platformvm.VMID` → `platformvm.Factory`
- `avm.VMID` → `avm.Factory`
- `evm.VMID` → `evm.Factory` (RPCChainVM wrapping EVM binary)

External plugin VMs scanned from `--plugin-dir`.

### 7.2 VM Registry (`vms/registry/`)

Dynamically discovers and loads VM plugins from the filesystem:
```go
type VMRegistry interface {
    Reload(ctx context.Context) (added, failed []ids.ID, err error)
}
```

Plugin detection: `sha256(binaryBytes)` as VMID; file name as default alias. On node startup and after `admin.LoadVMs()` call.

### 7.3 Runtime Manager (`vms/rpcchainvm/runtime/`)

Manages the lifecycle of external VM subprocesses:
```go
type Manager interface {
    Register(vmID ids.ID, config *subprocess.Config) error
    GetRuntime(vmID ids.ID) (Status, error)
    Stop(vmID ids.ID) error
    StopAll()
}
```

Each registered VM gets a `subprocess.Runtime` that tracks its PID, gRPC address, and health.
