# Node Lifecycle, Bootstrap, Configuration, Genesis & Upgrades

## 1. Purpose

This spec documents how an AvalancheGo process comes to life and dies: the OS
entry point, how raw flags / environment variables / config files become a
typed `node.Config`, how `node.Node` wires together every subsystem (logging,
database, networking, chain manager, API server, health, indexer, VMs), how the
three canonical networks (Mainnet / Fuji / Local) are defined by genesis and
parameters, how network upgrades are scheduled and how they gate behavior, and
how the node shuts down gracefully.

It is deliberately about **wiring, ordering, lifecycle, and config/genesis/upgrade
plumbing** — the deep internals of each subsystem live in their own specs (see
[Cross-references](#9-cross-references)).

## 2. Responsibilities & scope

`node.Node` is the central object that owns the lifetime of every long-lived
component of a running node. Its responsibilities:

- Parse the staking TLS certificate into a `NodeID`; construct the BLS staking
  signer and proof-of-possession.
- Construct and order the boot of: metrics, NAT, API server, database (with
  genesis-hash and ungraceful-shutdown bookkeeping), shared memory, message
  creator, validator manager, resource trackers, networking, health, chain
  manager, VM registry, info/admin APIs, chain/API aliases, indexer, profiler,
  and finally the P-Chain (which transitively creates X/C chains).
- Run the dispatch loop (HTTP server + P2P network) and tear everything down in
  a deterministic order on `Shutdown`.

Out of scope here: consensus engines, the VM execution logic, the networking
peer/handshake internals, the database engines — see sibling specs.

## 3. Package / file layout

| Path | Role |
|------|------|
| `main/main.go` | Process `main()`: build flags, build viper, handle `--version`, build `node.Config`, hand off to `app`. |
| `app/app.go` | `App` wrapper: permissions, log factory, fd-limit, signal handling, dispatch goroutine, exit code. |
| `node/node.go` | The central wiring (`New`, `Dispatch`, `Shutdown`, and all `initX` methods). 1900+ lines. |
| `node/beacon_manager.go` | Wraps the consensus router to detect when "enough" bootstrap beacons are connected. |
| `node/insecure_validator_manager.go` | When sybil protection is off, treats every connected peer as a primary-network validator. |
| `node/overridden_manager.go` | Validator-manager wrapper that maps all subnet calls onto a single subnet (primary network). |
| `config/keys.go` | String constants for every CLI/config key (e.g. `network-id`, `genesis-file`). |
| `config/flags.go` | `BuildFlagSet()` — registers every pflag with its default. |
| `config/viper.go` | `BuildViper()` — flags + env + config-file/content into a `*viper.Viper`; env prefix `avago`. |
| `config/config.go` | `GetNodeConfig()` and all `getX` helpers that turn viper into `node.Config`. |
| `config/node/config.go` | The `node.Config` struct and its embedded sub-configs. |
| `config/node/process_context.go` | `ProcessContext` (PID, API URI, staking address) written to disk for orchestration. |
| `genesis/config.go` | `Config`, `Allocation`, `Staker`, embedded mainnet/fuji/local configs, `GetConfig`. |
| `genesis/genesis.go` | `FromFile` / `FromFlag` / `FromConfig`, validation, P-Chain genesis construction, `VMGenesis`. |
| `genesis/params.go` | `StakingConfig`, `TxFeeConfig`, `Params`; `GetStakingConfig`/`GetTxFeeConfig`. |
| `genesis/genesis_{mainnet,fuji,local}.go` + `.json` | Per-network params (Go) and allocations/stakers (embedded JSON). |
| `genesis/bootstrappers.go` + `.json` | Default beacon node IDs/IPs per network; `GetBootstrappers`, `SampleBootstrappers`. |
| `genesis/checkpoints.go` + `.json` | Per-network, per-chain accepted-block checkpoints. |
| `genesis/validators.go` + `.json` | Recent validator node-ID sets per network. |
| `genesis/aliases.go` | Default chain/API/VM aliases (P/X/C). |
| `upgrade/upgrade.go` | `Config` of upgrade activation times; `Mainnet`/`Fuji`/`Default`; `IsXActivated`. |
| `version/constants.go` | `Current` version, `MinimumCompatibleVersion`, `RPCChainVMProtocol`, DB version. |
| `version/application.go`, `version/compatibility.go` | Version comparison and peer-compatibility logic. |

## 4. Core types

### `node.Config` — `config/node/config.go:130`
A flat struct embedding sub-configs (`HTTPConfig`, `IPConfig`, `StakingConfig`,
`TxFeeConfig`, `StateSyncConfig`, `BootstrapConfig`, `DatabaseConfig`) plus
`UpgradeConfig`, `GenesisBytes`, `AvaxAssetID`, `NetworkID`, `NetworkConfig`,
`SubnetConfigs`, `ProvidedFlags`, `ProcessContextFilePath`, etc. Notable nested
types:
- `StakingConfig` — `config/node/config.go:73` (embeds `genesis.StakingConfig`;
  adds `SybilProtectionEnabled`, `StakingTLSCert`, `StakingSignerConfig`).
- `StakingSignerConfig` — `config/node/config.go:84` (BLS signer selection:
  ephemeral / key-content / key-path / RPC endpoint).
- `BootstrapConfig` — `config/node/config.go:97` (beacon connection timeout,
  ancestors limits, `Bootstrappers []genesis.Bootstrapper`).
- `DatabaseConfig` — `config/node/config.go:115` (`Name`, `Path`, `ReadOnly`).

### `node.Node` — `node/node.go:292`
Owns: `Log`/`LogFactory`, `ID`, `StakingTLSSigner`/`StakingTLSCert`/`StakingSigner`,
`DB`, `chainRouter`, `chainManager`, `Net`, `APIServer`, `health`, `indexer`,
`sharedMemory`, `msgCreator`, `timeoutManager`, `benchlistManager`,
`bootstrappers` (beacon set), `vdrs` (live validator set), `VMManager`,
`VMRegistry`, `resourceManager`/`resourceTracker`, `MetricsGatherer`,
`onSufficientlyConnected`, and the shutdown atomics (`shutdownOnce`,
`shuttingDown`, `shuttingDownExitCode`).

### `genesis.Config` — `genesis/config.go:89`
`NetworkID`, `Allocations`, `StartTime`, `InitialStakeDuration[Offset]`,
`InitialStakedFunds`, `InitialStakers`, `CChainGenesis`, `Message`. Backed by
`Allocation` (`:38`), `LockedAmount` (`:33`), `Staker` (`:67`).

### `genesis.Params` / `StakingConfig` / `TxFeeConfig` — `genesis/params.go:45,15,38`
Per-network staking economics and fee configuration.

### `upgrade.Config` — `upgrade/upgrade.go:91`
One `time.Time` per network upgrade (ApricotPhase1..6/Pre6/Post6, Banff,
Cortina, Durango, Etna, Fortuna, Granite, Helicon), plus `GraniteEpochDuration`
and a couple of height/vertex anchors.

### `version.Application` — `version/application.go:14`
`{Name, Major, Minor, Patch}` with `Compare`. `version.Current` is the running
version; `version.Compatibility` (`version/compatibility.go:14`) decides peer
acceptability.

## 5. Startup & shutdown lifecycle

```
OS process
  │
  ▼
main.main()                                   main/main.go:21
  ├─ evm.RegisterAllLibEVMExtras()            (register coreth libevm hooks)
  ├─ config.BuildFlagSet()                    config/flags.go:393
  ├─ config.BuildViper(fs, os.Args)           config/viper.go:29   (flags+env+file)
  ├─ handle --version / --version-json        (print & exit)
  ├─ config.GetNodeConfig(v)  ───────────────► builds typed node.Config
  │     ├─ NetworkID  (constants.NetworkID)
  │     ├─ Logging / DB / IP / Staking config
  │     │     └─ staking TLS cert: ephemeral | content | file(auto-generate)
  │     ├─ UpgradeConfig  (upgrade.GetConfig|file)
  │     ├─ GenesisData    (FromConfig|FromFile|FromFlag → bytes + AvaxAssetID)
  │     └─ BootstrapConfig (SampleBootstrappers(5) or flag IPs+IDs)
  └─ app.New(nodeConfig) ; app.Run(app)       app/app.go:48,89
        │
        ▼
app.New                                       app/app.go:48
  ├─ perms.ChmodR(db dir, log dir)            (0700)
  ├─ logging.NewFactory + Make("main")
  ├─ ulimit.Set(FdLimit)
  └─ node.New(&config, logFactory, log) ──────► constructs the Node (below)

node.New (ordered wiring)                     node/node.go:128
  1. ParseCertificate → ID = NodeIDFromCert            :133
  2. newStakingSigner(StakingSignerConfig) → BLS       :148 / :1774
  3. NewProofOfPossession ; log "initializing node"    :153
  4. VMAliaser + VMManager                              :173
  5. initBootstrappers()  (beacon validators.Manager)   :183 / :853
  6. trace.New (tracer)                                 :188
  7. initMetrics()        (prefix + meterdb gatherers)  :193 / :921
  8. initNAT()            (UPnP/NAT-PMP or no-router)    :197 / :930
  9. initAPIServer()      (bind HTTP port, maybe TLS)   :198 / :948
 10. initMetricsAPI()     (/ext/metrics)                :202 / :1274
 11. initDatabase()       (factory, meterdb, genesis    :206 / :757
        hash check, ungracefulShutdown marker)
 12. initSharedMemory()                                  :210 / :1266
 13. message.NewCreator   (shared by net+chains+engine) :225
 14. vdrs = validators.NewManager()                      :234
        (wrapped by overriddenManager if sybil off)
 15. initResourceManager() ; initCPUTargeter ;           :239
        initDiskTargeter
 16. initNetworking(reg)  (listener, public IP/NAT,     :244 / :421
        TLS config, chainRouter, benchlist, beacon/
        insecure managers, network.NewNetwork)
 17. initEventDispatchers() (Block/Tx/Vertex acceptors) :248 / :868
 18. initHealthAPI()      (network/router/db/disk/bls/  :253 / :1412
        futureupgrade checks)  — before chain manager
 19. addDefaultVMAliases()                               :256 / :1040
 20. initChainManager(AvaxAssetID)  (timeoutManager,    :259 / :1056
        chainRouter.Initialize, subnets, chains.New)
 21. initVMs()            (register P/X/C factories,     :262 / :1184
        VMRegistry.Reload plugins from disk)
 22. initAdminAPI()                                      :265 / :1314
 23. initInfoAPI()                                       :268 / :1367
 24. initChainAliases / initAPIAliases (from genesis)   :271 / :1666,:1693
 25. initIndexer()        (prefixdb, registrant)        :277 / :878
 26. health.Start(HealthCheckFreq) ; initProfiler()     :281
 27. initChains(GenesisBytes)  → chainManager           :285 / :906
        .StartChainCreator(P-Chain) → X/C created
        │
        ▼
app.Start()                                   app/app.go:141
  └─ goroutine: node.Dispatch()               node/node.go:667
        ├─ writeProcessContext()  (PID/URI/staking addr → file)
        ├─ go APIServer.Dispatch()            (serve HTTP; Shutdown(1) on fail)
        ├─ go warn-if-not-connected timer     (BootstrapBeaconConnectionTimeout)
        ├─ Net.ManuallyTrack(state-sync + bootstrap peers)
        └─ Net.Dispatch()  (blocks; serving P2P) → Shutdown(1) on return
                                                  └─ removes process-context file

signal handling                               app/app.go:89-128
  SIGINT / SIGTERM → app.Stop() → node.Shutdown(0)
  SIGABRT          → print goroutine stack trace to stderr

node.Shutdown(exitCode)  (idempotent)         node/node.go:1820
  └─ shutdownOnce.Do(shutdown):
       register "shuttingDown" health check; sleep ShutdownWait
       StakingSigner.Shutdown · resourceManager.Shutdown
       timeoutManager.Stop · chainManager.Shutdown · benchlistManager.Shutdown
       profiler.Shutdown · Net.StartClose · APIServer.Shutdown
       portMapper.UnmapAllPorts · ipUpdater.Stop · indexer.Close
       runtimeManager.Stop  (plugin VM processes)
       DB.Delete(ungracefulShutdown) · DB.Close
       tracer.Close
app.ExitCode() = node.ExitCode()              (blocks on exitWG, returns code)
```

## 6. Component boundaries & relationships

`node.New` is the single composition root. Key wiring relationships and the
interfaces crossed:

- **app ↔ node**: `app` only knows the `App` interface (`Start`/`Stop`/`ExitCode`,
  `app/app.go:33`) and `*node.Node`. `app.Stop()` calls `node.Shutdown(0)`;
  panics in `Dispatch` are recovered and logged (`app/app.go:144`).
- **message.Creator is shared** between the network layer, the chain manager,
  and the engines, so it is built (`:225`) after metrics but before networking
  and chains — the ordering comment at `node/node.go:212` is load-bearing.
- **Validator manager (`n.vdrs`)** is created once and threaded into networking,
  benchlist, CPU/disk targeters, chain manager, and the P-Chain VM. If sybil
  protection is disabled it is wrapped by `overriddenManager`
  (`node/overridden_manager.go:19`) so every subnet query resolves to the
  primary network, and peers self-register via `insecureValidatorManager`
  (`node/insecure_validator_manager.go:24`) using a dummy txID derived from the
  nodeID.
- **Beacon connectivity gate**: `initBootstrappers` builds a separate
  `validators.Manager` of beacon IDs (`node/node.go:853`). The consensus router
  is wrapped in `beaconManager` (`node/beacon_manager.go:19`) which closes
  `onSufficientlyConnected` once `(3*numBootstrappers + 3) / 4` beacons are
  connected (`node/node.go:600`). When there are zero beacons the channel is
  closed immediately.
- **chainManager → APIServer / indexer**: the chain manager notifies registrants
  when chains are created. Both `APIServer` (`node/node.go:1179`) and the
  `indexer` (`:899`) are registered, so new chains automatically get HTTP routes
  and indexing.
- **Health API must precede the chain manager** (`node/node.go:251`) because
  `n.health` is passed into `chains.New`. Health checks registered:
  `network`, `router`, `database`, `diskspace` (fatal-shutdown below a required
  %), `bls` (node's BLS key matches its validator-set entry), and
  `futureupgrade` (described in §8).
- **VMs**: `initVMs` registers the built-in `platformvm`, `avm`, and `coreth`
  (EVM) factories (`node/node.go:1198`), then `VMRegistry.Reload` loads any
  rpcchainvm plugins from `PluginDir`. When sybil protection is off the P-Chain
  gets its own throwaway validator manager so the network's validator set is
  driven purely by connections (`node/node.go:1192`).
- **Database genesis binding**: `initDatabase` computes
  `ComputeHash256(GenesisBytes)`, persists it under `genesisID` on first run,
  and refuses to start if a later genesis hash disagrees (`node/node.go:825`).
  It also sets an `ungracefulShutdown` marker that is only deleted in
  `shutdown()` — a present marker on next boot logs a warning.

## 7. Key behaviors, invariants, edge cases

- **`Shutdown` is idempotent** (`node/node.go:1820`): `shuttingDown.Swap(true)`
  records the exit code only on the first call; `shutdownOnce` guarantees the
  teardown body runs once and all callers block until it completes.
- **Self-triggered shutdowns**: API server failure, P2P dispatch return,
  profiler failure, indexer shutdown hook, and the low-disk health check all
  call `Shutdown(1)` (except indexer/SIGINT which use 0).
- **Public-IP resolution** (`node/node.go:458`): precedence is explicit
  `PublicIP` → `PublicIPResolutionService` (with a dynamic updater) → NAT router
  `ExternalIP()`. A private resulting IP logs a "not publicly discoverable"
  warning.
- **MacOS firewall note**: binding `ListenHost` to `::1`/`127.0.0.1` avoids the
  per-binary firewall prompt (long comment at `node/node.go:421`).
- **Sybil protection cannot be disabled on Mainnet/Fuji** and, when disabled,
  requires a non-zero `SybilProtectionDisabledWeight`
  (`config/config.go:783`).
- **Genesis hash mismatch** between DB and computed genesis is fatal
  (`node/node.go:825`); guards against pointing an existing data dir at the
  wrong network.
- **Local network start time auto-advances**: `LocalConfig.StartTime` is rolled
  forward in 9-month chunks so a local genesis is never "in the future"
  (`genesis/config.go:25,211`, `getRecentStartTime` `:271`).
- **Unknown custom networks** fall back to a copy of `LocalConfig`/`LocalParams`/
  `upgrade.Default` with the requested `NetworkID`
  (`genesis/config.go:231`, `genesis/params.go:58`, `upgrade/upgrade.go:210`).

## 8. Configuration, genesis & upgrade details

### 8.1 Configuration precedence (viper)
`BuildViper` (`config/viper.go:29`) constructs precedence as, highest first:

1. **Command-line flags** (`fs.Parse`, then `v.BindPFlags`).
2. **Environment variables** — `v.AutomaticEnv()` with prefix `avago` and the
   dash→underscore replacer, so `--network-id` ⇄ `AVAGO_NETWORK_ID`
   (`config/viper.go:18-25`).
3. **Config file / config-file-content** — chosen by `ConfigContentKey`
   (base64, with `config-file-content-type`) or `ConfigFileKey`
   (`config/viper.go:46`).
4. **Built-in defaults** declared in `config/flags.go` (e.g. `network-id`
   defaults to `mainnet`, `config/flags.go:105`).

`GetNodeConfig` (`config/config.go:1335`) then materializes the typed config;
`ProvidedFlags` records exactly which keys the user set (logged at startup).

### 8.2 The three networks

| | Mainnet (`1`) | Fuji/Testnet (`5`) | Local (`12345`) |
|---|---|---|---|
| ID constant | `constants.MainnetID` | `FujiID`/`TestnetID` | `LocalID` |
| Genesis JSON | `genesis_mainnet.json` (~3.5 MB) | `genesis_fuji.json` | `genesis_local.json` |
| Params | `MainnetParams` (`genesis_mainnet.go:22`) | `FujiParams` | `LocalParams` (`genesis_local.go:40`) |
| Upgrade cfg | `upgrade.Mainnet` | `upgrade.Fuji` | `upgrade.Default` (all at `InitiallyActiveTime`) |
| MinStakeDuration | 2 weeks | 24h | 24h |
| MinValidatorStake | 2 KiloAvax | 2 KiloAvax | 2 KiloAvax |
| MaxValidatorStake | 3 MegaAvax | 3 MegaAvax | 3 MegaAvax |
| Uptime requirement | 80% | 80% | 80% |
| SupplyCap | 720 MegaAvax | 720 MegaAvax | 720 MegaAvax |
| TxFee / CreateAssetTxFee | 1 mAVAX / 10 mAVAX | 1 mAVAX / 10 mAVAX | 1 mAVAX / 1 mAVAX |
| Custom genesis allowed | No (`FromFile`/`FromFlag` reject) | No | No |

Local also ships well-known test keys (`VMRQKey`, `EWOQKey`,
`genesis_local.go:24`).

### 8.3 Genesis parsing, validation & allocations
- **Selection** (`getGenesisData`, `config/config.go:932`): `genesis-file-content`
  → `genesis-file` → built-in per-network config. For Mainnet/Fuji/Local,
  `FromFile`/`FromFlag` **error** if a custom genesis is supplied
  (`genesis/genesis.go:227`) — these networks are immutable.
- **Validation** (`validateConfig`, `genesis/genesis.go:136`): networkID match,
  initial supply > 0, start time not in the future, non-zero stake duration ≤
  `MaxStakeDuration`, ≥1 staker, offset fits in the duration, every initially
  staked address is unique and present in allocations, total locked ≥ number of
  stakers, non-empty C-Chain genesis.
- **Construction** (`FromConfig`, `genesis/genesis.go:294`): builds the AVM
  genesis (the single "AVAX" asset, 9 decimals) → derives `AvaxAssetID`
  (`:341`); builds P-Chain UTXOs from unlocked + locked allocations; splits the
  initially-staked allocations across the genesis validators
  (`splitAllocations`, `:469`); emits the X-Chain and C-Chain `CreateChainTx`s;
  serializes the whole thing as the **P-Chain genesis bytes** — which *is* the
  network genesis. `VMGenesis` (`:531`) later extracts the X/C chain txs (and
  thus their chain IDs) used by `initChainManager`.

### 8.4 Bootstrappers / beacons & checkpoints
- Defaults are embedded JSON keyed by network name (`bootstrappers.json`,
  `checkpoints.json`, `validators.json`). `GetBootstrappers` /
  `SampleBootstrappers` (`genesis/bootstrappers.go:39,45`) return beacon
  `{ID, IP}` pairs.
- `getBootstrapConfig` (`config/config.go:610`): if neither `bootstrap-ips` nor
  `bootstrap-ids` is set, sample 5 default beacons; otherwise both must be set
  with equal length and are paired by index.
- `node.initBootstrappers` (`node/node.go:853`) loads them into a beacon
  `validators.Manager` with weight 1 each (TxID/BLS are intentionally unused).
  The beacon set also seeds the P-Chain as `CustomBeacons` (`:914`).

### 8.5 Staking key/cert & BLS signer
- **TLS cert** (`getStakingTLSCert`, `config/config.go:752`): ephemeral
  (`--staking-ephemeral-cert-enabled`) → key+cert content (base64) → key+cert
  files; if file paths are unset, a key/cert pair is **auto-generated**
  (`InitNodeStakingKeyPair`, `:739`). The cert is parsed in `node.New` to derive
  the `NodeID` (`node/node.go:133`).
- **BLS signer** (`newStakingSigner`, `node/node.go:1774`): exactly one of
  ephemeral / base64 key content / RPC endpoint / key path may be set
  (`getStakingSignerConfig`, `config/config.go:838`). With none set, a key is
  loaded from the default path or persisted-new
  (`localsigner.FromFileOrPersistNew`).

### 8.6 Network upgrades and how they gate behavior
`upgrade.Config` (`upgrade/upgrade.go:91`) is a list of activation timestamps,
selected by `upgrade.GetConfig(networkID)` (`:204`) or overridden via
`--upgrade-file[-content]` (only for non-standard networks,
`config/config.go:894`). `Validate()` enforces monotonically non-decreasing
times (`:112`).

Selected mainnet activations (`upgrade/upgrade.go:19`):

| Upgrade | Mainnet time | Gates (examples) |
|---------|--------------|------------------|
| ApricotPhase1–5 | 2021 | fee/throttling, staking, atomic-tx, P-Chain height anchor |
| Banff | 2022-10-18 | Banff block format |
| Cortina | 2023-04-25 | X-Chain linearization (stop-vertex ID anchor) |
| Durango | 2024-03-06 | Warp/messaging changes |
| Etna | 2024-12-16 | L1 / ACP-77 validator changes |
| Fortuna | 2025-04-08 | dynamic fees |
| Granite | 2025-11-19 | passed to `network.NewNetwork` (`node/node.go:632`); `GraniteEpochDuration` 5m |
| Helicon | unscheduled (`9999`) | future |

Each `IsXActivated(t)` is `!t.Before(XTime)` (`:144+`). The `Config` is threaded
into the network, chain manager (`Upgrades:`, `node/node.go:1165`), and every VM
factory, so subsystems branch on activation by comparing the current/block time.

**Future-upgrade health check** (`node/node.go:1532`): the node samples peers'
advertised `UpgradeTime`, computes the stake-weighted mode, and if the network
majority (≥50% weight) advertises a *later* upgrade time than this node's local
`GraniteTime`, it concludes the binary is out of date. It logs at escalating
severity and returns `errUpgradeWithinTheHour` etc. as the window closes
(`node/node.go:122`, thresholds at `:1588`), and publishes an
`avalanche_upgrade_time_until` gauge.

**Version compatibility** (`version/compatibility.go:30`): a peer is acceptable
if its major ≥ ours and it is ≥ the min-compatible version (which tightens from
`PrevMinimumCompatibleVersion` to `MinimumCompatibleVersion` at the upgrade
time). `version.Current` is `1.14.2`; `RPCChainVMProtocol` is `45`
(`version/constants.go:18,26`).

### 8.7 Selected important flags

| Flag (`config/keys.go`) | Default | Effect |
|---|---|---|
| `network-id` | `mainnet` | Selects genesis/params/upgrade/bootstrappers. |
| `genesis-file` / `genesis-file-content` | unset | Custom genesis (non-standard nets only). |
| `upgrade-file` / `upgrade-file-content` | unset | Custom upgrade times (non-standard nets only). |
| `config-file` / `config-file-content` | unset | Source viper config (precedence above defaults). |
| `data-dir` | platform default | Root for DB and logs. |
| `staking-tls-cert-file` / `staking-tls-key-file` | data-dir | TLS identity; auto-generated if absent. |
| `staking-ephemeral-cert-enabled` | false | Throwaway identity. |
| `sybil-protection-enabled` | true | Off ⇒ peers self-register as validators. |
| `public-ip` / `public-ip-resolution-service` | unset | Override NAT IP discovery. |
| `http-host` / `http-port` | localhost / 9650 | API server bind. |
| `staking-port` (`ListenPort`) | 9651 | P2P bind. |
| `index-enabled` | false | Enable tx/block indexer. |
| `plugin-dir` | data-dir/plugins | rpcchainvm plugin discovery. |
| `track-subnets` | empty | Subnets to validate/track. |

## 9. Cross-references

- [overview.md](./overview.md) — system map and how the node fits in.
- [networking.md](./networking.md) — the P2P stack `node.initNetworking` builds.
- [chains.md](./chains.md) — the chain manager `initChainManager`/`initChains` wires.
- [consensus.md](./consensus.md), [simplex.md](./simplex.md) — engines behind the chains.
- [vm-framework.md](./vm-framework.md) — VM registry/factories `initVMs` registers.
- [platformvm.md](./platformvm.md), [avm.md](./avm.md), [evm.md](./evm.md) — the built-in VMs and their genesis.
- [database.md](./database.md) — engines behind `initDatabase`.
- [api.md](./api.md) — the HTTP/admin/info/health/metrics surfaces wired here.
- [primitives.md](./primitives.md) — `ids`, validators, BLS, staking certificates.
```
