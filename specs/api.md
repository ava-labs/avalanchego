# AvalancheGo — HTTP/JSON-RPC API, Indexer & Wallet

## 1. Purpose

This document specifies the **node-level API surface** of AvalancheGo: the single multiplexing HTTP server, the framework chains/VMs use to register handlers, the node-wide endpoints (`/ext/info`, `/ext/admin`, `/ext/health`, `/ext/metrics`), the health-check subsystem, the optional accepted-container indexer, and the Go wallet SDK. It explains *how the framework works and where the boundaries are* — it does **not** enumerate the JSON-RPC methods of each VM. Those belong to the VM specs ([platformvm.md](platformvm.md), [avm.md](avm.md), [evm.md](evm.md)).

A reader should come away understanding: that AvalancheGo runs exactly one HTTP server multiplexing every endpoint, how a chain's VM handlers get mounted under `/ext/bc/<chainID>`, how the request router dispatches (header-route vs legacy path-route), how readiness/health/liveness differ and how their worker model behaves, what the indexer stores and exposes, and how the wallet's backend/builder/signer triad builds and issues transactions.

---

## 2. Responsibilities & Scope

**In scope:**
- The HTTP server (`api/server/`): construction, lifecycle, routing, CORS, allowed-hosts (DNS-rebinding protection), per-call metrics, tracing, the bootstrap-rejection middleware.
- Handler registration framework: `PathAdder`, `RegisterChain`, header routes, aliases, the `chains.Registrant` boundary.
- Node-wide services: `api/info/`, `api/admin/`, `api/health/`, `api/metrics/`, plus shared types in `api/common_args_responses.go` and `api/traced_handler.go`.
- The health subsystem in detail (`api/health/`): the three workers, tags, monotonic readiness checks, metrics.
- The optional indexer (`indexer/`): what is indexed, ordering invariants, the index API.
- The wallet SDK (`wallet/`): backend/builder/signer pattern, the primary-network wallet.

**Out of scope (cross-referenced):**
- VM-specific endpoint semantics — see [platformvm.md](platformvm.md), [avm.md](avm.md), [evm.md](evm.md).
- The chain-creation lifecycle that *calls* `RegisterChain` — see [chains.md](chains.md).
- Networking/peer concepts surfaced by `info.peers` — see [networking.md](networking.md).
- Databases backing the indexer — see [database.md](database.md).

---

## 3. Package / File Layout

```
api/
  common_args_responses.go   Shared request/response structs (GetBlock, GetTx, GetUTXOs, JSONAddress…)
  traced_handler.go          OpenTelemetry wrapper around any http.Handler
  server/
    server.go                Server interface, server impl, RegisterChain, middleware, CORS wrapper
    router.go                router: mux + header-route table + alias table
    allowed_hosts.go         Host-header allow-list (DNS rebinding defense)
    metrics.go               Per-base calls / calls_processing / calls_duration
  info/
    service.go               Info JSON-RPC service (unprivileged node info)
    client.go                Go client
  admin/
    service.go               Admin JSON-RPC service (privileged: profiling, aliases, logger, DB)
    key_value_reader.go, client.go
  health/
    health.go                Health interface; wraps 3 workers (readiness/health/liveness)
    worker.go                worker: periodic check runner, tags, metrics
    checker.go               Checker / CheckerFunc interface
    handler.go               GET + JSON-RPC POST HTTP handlers
    service.go               JSON-RPC Service (Readiness/Health/Liveness methods)
    result.go                Result struct
    README.md                Authoritative description of the health model
  metrics/
    multi_gatherer.go        MultiGatherer (register/deregister sub-gatherers)
    prefix_gatherer.go       Namespaces metrics by prefix
    label_gatherer.go        Namespaces metrics by an added label
  connectclient/
    client.go                connect-rpc (gRPC-over-HTTP) client + route-header interceptor

indexer/
  indexer.go                 Indexer: chains.Registrant; creates per-chain indices + endpoints
  index.go                   index: append-only accepted-container store (acceptance order)
  service.go                 index JSON-RPC service (GetContainerByIndex, GetContainerByID…)
  container.go, codec.go     Container record + codec
  client.go                  Go client for an index
  examples/                  Runnable polling examples (p-chain, x-chain-blocks)

wallet/
  subnet/primary/            MakeWallet / MakePWallet, FetchState, primary-network Wallet
    common/                  Options, UTXO cache, spend helpers
  chain/{p,x,c}/             Per-chain wallet = builder + signer + backend + client
    p/builder, p/signer, p/wallet   (split packages for P-chain)
    x/builder, x/signer             (split packages for X-chain)
```

---

## 4. Core Types

### 4.1 `Server` interface & implementation

`api/server/server.go:60` defines the contract:

```go
type Server interface {
    PathAdder                // AddRoute, AddAliases
    PathAdderWithReadLock    // …WithReadLock variants
    Dispatch() error         // start serving
    RegisterChain(chainName string, ctx *snow.ConsensusContext, vm common.VM)
    Shutdown() error
}
```

- `PathAdder` (`server.go:41`): `AddRoute(handler, base, endpoint)` and `AddAliases`. This is the **only** way arbitrary subsystems mount handlers. The indexer holds just a `server.PathAdder`, not the full server.
- The concrete `server` (`server.go:80`) wraps a single `*http.Server` whose handler is built in `New` (`server.go:101`): `wrapHandler(router, …)` → host filter → CORS → adds the `node-id` response header. The server uses `h2c` (HTTP/2 cleartext) so connect-rpc clients work without TLS, capped at `maxConcurrentStreams = 64` (`server.go:33`).
- `Dispatch()` (`server.go:149`) is just `srv.Serve(listener)`. `Shutdown()` (`server.go:289`) closes the listener, calls graceful `Shutdown` with `shutdownTimeout`, then forces `Close`.
- All URLs are rooted at `baseURL = "/ext"` (`server.go:32`).

### 4.2 The `router`

`api/server/router.go:25` is the dispatch core. It holds three independent maps under a `sync.RWMutex`:

- `routes map[string]map[string]http.Handler` — legacy **path-based** routing (`base` → `endpoint` → handler), also mirrored into a `gorilla/mux` router.
- `headerRoutes map[string]http.Handler` — **header-based** routing keyed by the value of the `Avalanche-Api-Route` header (`HTTPHeaderRoute`, `router.go:17`).
- `aliases` + `reservedRoutes` — alias bookkeeping.

`ServeHTTP` (`router.go:49`) is the fork point:

```go
route, ok := request.Header[HTTPHeaderRoute]
if !ok {                          // no header → legacy mux path routing
    r.router.ServeHTTP(...)
} else if len(route) < 1 {
    400
} else if handler := r.headerRoutes[route[0]]; handler == nil {
    404
} else {
    handler.ServeHTTP(...)        // header route (used by connect-rpc VMs)
}
```

### 4.3 Handler registration: chain VMs

`RegisterChain` (`server.go:153`) is the boundary where a chain's VM contributes its endpoints. It does two things:

1. **Path-route handlers** — calls `vm.CreateHandlers(ctx)` under the chain's `ctx.Lock`, returning `map[extension]http.Handler`. Each is mounted under `defaultEndpoint = path.Join("bc", ctx.ChainID.String())` (`server.go:169`), i.e. `/ext/bc/<chainID><extension>`. Each route URL extension is validated with `url.ParseRequestURI`; malformed ones are skipped, not fatal.
2. **Header-route handler** — calls `vm.NewHTTPHandler(ctx)`; if non-nil it is registered in the header-route table under the chain ID string via `router.AddHeaderRoute` (`server.go:206`). This is the connect-rpc / gRPC-style path; clients select it by sending `Avalanche-Api-Route: <chainID>`.

Both handlers pass through `wrapMiddleware` (`server.go:224`): optional tracing → `rejectMiddleware` → metrics wrapper.

`rejectMiddleware` (`server.go:260`) is an important invariant: **API calls to a chain are rejected with HTTP 503 until that chain reaches `snow.NormalOp`** (i.e. finished state-sync/bootstrap). It re-reads `ctx.State.Get().State` on every request, so handlers go live automatically once bootstrapping completes.

### 4.4 Handler registration: node-wide services

Node-wide services are mounted directly via `AddRoute(handler, base, "")` in `node/node.go`. They are **plain `gorilla/rpc/v2` JSON-RPC servers** (except metrics and the health GET handlers):

| URL | base / endpoint | Source |
|-----|-----------------|--------|
| `/ext/info` | `info`, `""` | `info.NewService` → `node.go:1380` |
| `/ext/admin` | `admin`, `""` | `admin.NewService` → `node.go:1320` |
| `/ext/metrics` | `metrics`, `""` | `promhttp.HandlerFor` → `node.go:1302` |
| `/ext/health` | `health`, `""` | `health.NewGetAndPostHandler` → `node.go:1631` |
| `/ext/health/readiness` | `health`, `/readiness` | `health.NewGetHandler(Readiness)` → `node.go:1640` |
| `/ext/health/health` | `health`, `/health` | `node.go:1649` |
| `/ext/health/liveness` | `health`, `/liveness` | `node.go:1658` |
| `/ext/index/<chain>/<type>` | `index/<name>`, `/<endpoint>` | indexer → `indexer.go:340` |

### 4.5 JSON-RPC 2.0 codec

Every JSON-RPC service uses `gorilla/rpc/v2` with the custom codec from `utils/json.NewCodec()` (`utils/json/codec.go:28`). This codec lowercases the first letter of the method name, so the exported Go method `Info.GetNodeID` is invoked over the wire as `info.getNodeID`. Methods follow the gorilla signature `func(*http.Request, *Args, *Reply) error`. A request looks like:

```json
{"jsonrpc":"2.0","id":1,"method":"info.getNodeID","params":{}}
```

### 4.6 Health `Checker`

`api/health/checker.go:11`:

```go
type Checker interface {
    HealthCheck(context.Context) (interface{}, error)   // details + nil error = healthy
}
type CheckerFunc func(context.Context) (interface{}, error)
```

A subsystem reports healthy by returning `nil` error; the `interface{}` is JSON-marshalled into `Result.Details` (`api/health/result.go:19`). Results also carry `Timestamp`, `Duration`, `ContiguousFailures`, and `TimeOfFirstFailure`.

---

## 5. Request Routing & Registration Flow

### 5.1 Request routing

```
HTTP request
   │
   ▼  http.Server (h2c, HTTP/1.1 or HTTP/2 cleartext)
wrapHandler:  set "node-id" header
   │  cors.Handler (AllowedOrigins, AllowCredentials)
   │  allowedHostsHandler (Host header allow-list)
   ▼
router.ServeHTTP
   │
   ├── has "Avalanche-Api-Route" header? ── yes ──► headerRoutes[value]  (VM connect-rpc handler)
   │                                                  └─ wrapMiddleware (trace→reject→metrics)
   └── no ──► gorilla/mux match on "/ext/<base><endpoint>"
                 ├─ /ext/bc/<chainID>...   chain VM handler (reject-until-NormalOp + metrics)
                 ├─ /ext/info|admin        gorilla JSON-RPC service
                 ├─ /ext/health[/...]      GET→200/503 ; POST→JSON-RPC
                 ├─ /ext/metrics           Prometheus text exposition
                 └─ /ext/index/<chain>/... index JSON-RPC service
```

### 5.2 Chain handler-registration flow

```
chains.Manager creates chain  (see chains.md)
   │  ctx reaches the registrant list
   ▼
manager.RegisterChain → fan-out to every Registrant  (manager.go:605 AddRegistrant)
   ├──► server.RegisterChain(name, ctx, vm)            (registered at node.go:1179)
   │       ├─ vm.CreateHandlers()  → mount under /ext/bc/<chainID><ext>
   │       └─ vm.NewHTTPHandler()  → header route keyed by chainID
   └──► indexer.RegisterChain(name, ctx, vm)           (registered at node.go:899)
           └─ if primary network & indexing enabled: create block/tx/vtx indices + /ext/index endpoints
```

Both `server` and `indexer` implement `chains.Registrant` (`chains/registrant.go:12`). The node adds them with `n.chainManager.AddRegistrant(...)`, so chain creation pushes into the API layer rather than the API layer polling.

---

## 6. Component Boundaries & Relationships

### server ↔ chains.Manager
The boundary is the `chains.Registrant` interface (`chains/registrant.go:12`). `chains.Manager` knows nothing about HTTP; it calls `RegisterChain(name, ctx, vm)` on each registrant after a chain is created but **before it processes messages**. The server is one such registrant (`node.go:1179`). The server's only inputs are the chain name, the `*snow.ConsensusContext` (for the chain ID, lock, and bootstrap state), and the VM (for `CreateHandlers`/`NewHTTPHandler`). The server never reaches into chain internals.

### server ↔ each VM service
A VM exposes its API two ways (`server.go:155`, `server.go:191`):
- **`CreateHandlers()`** returns path-mounted handlers (the classic `gorilla/rpc` JSON-RPC service at `/ext/bc/<chainID>`, e.g. `vms/platformvm/service.go`). See the VM specs for method semantics.
- **`NewHTTPHandler()`** returns a single handler reached by header routing — the connect-rpc transport. Clients use `connectclient.SetRouteHeaderInterceptor` (`connectclient/client.go:22`) to set `Avalanche-Api-Route: <chainID>`.

The server wraps every VM handler with `rejectMiddleware` so the chain's bootstrap state gates access, plus per-chain metrics and optional tracing. This is the only behavior the server imposes on VM endpoints.

### health ↔ subsystems
`health.Health` (`health.go:34`) is a pure registry: subsystems call `RegisterHealthCheck("network", n.Net, ApplicationTag)` etc. (`node.go:1432`). The health subsystem invokes each `Checker.HealthCheck` periodically and never holds its own locks during the call (`worker.go:226`), so a checker may safely grab the subsystem's locks without deadlock. Subsystems registered as health checks include `network`, `router`, `database`, `diskspace`, `bls`, `futureupgrade`, and `shuttingDown` (`node.go:1432`–`1840`), all tagged `application`.

### indexer ↔ engine
The indexer never talks to the HTTP server beyond `PathAdder`. It learns about accepted containers by registering as an `snow.Acceptor` with the per-chain `AcceptorGroup`s (`indexer.go:326`). The consensus engine pushes `Accept(ctx, containerID, bytes)` to the index synchronously, in acceptance order, **before** the VM commits the container (`index.go:41`). The indexer mounts a read-only JSON-RPC service per index via `PathAdder.AddRoute` (`indexer.go:340`).

### wallet ↔ node
The wallet is a **client-side SDK**, fully decoupled from the node process. It talks to a node over the standard P/X/C JSON-RPC endpoints (and EVM RPC) using `MakeWallet(ctx, uri, …)` (`wallet/subnet/primary/wallet.go:80`). It builds and signs transactions locally and only issues finished bytes.

---

## 7. Key Behaviors, Invariants & Security

### Health model (authoritative: `api/health/README.md`)
Three independent workers (`health.go:61`) sharing one `checks_failing` gauge:

- **Readiness** — registered via `RegisterMonotonicCheck` (`health.go:85` → `worker.go:127`). A monotonic check runs only until it first passes; thereafter it caches and returns the success result forever. Used for one-time startup gates.
- **Health** — flips freely based on the subsystem's heuristic (`health.go:88`).
- **Liveness** — indicates an unrecoverable state requiring a restart (`health.go:92`).

Worker behavior (`worker.go`):
- `Start(ctx, freq)` launches one goroutine that runs all checks immediately, then every `freq` (default **30s**, set in `node`). `Start` is idempotent via `sync.Once` (`worker.go:172`).
- Each iteration clones the check map and runs **every check in its own goroutine** (`worker.go:214`); a check added mid-iteration runs next time.
- A newly registered check is immediately recorded as failing with `Result{Error:"not yet run"}` (`result.go:13`, `worker.go:112`). So a node is unhealthy until checks actually run and pass.
- `ContiguousFailures` and `TimeOfFirstFailure` accumulate across iterations (`worker.go:244`).
- On `Stop` (`worker.go:196`) in-flight checks finish, the goroutine exits, and checks never run again.

**Tags** (`health.go:21`): every check implicitly gets `AllTag = "all"`. Registering with `AllTag` explicitly is an error (`worker.go:80`, prevents metric double-count). The special `ApplicationTag = "application"` makes a check appear in **every** tag-filtered query — application checks cannot be filtered out (`worker.go:153`). `Results(tags...)` always unions in the application tag; with no tags it returns the `all` set. A result set is healthy iff every included check's `Error == nil` (`worker.go:166`).

**HTTP semantics** (`handler.go`): `GET /ext/health[/...]` returns the JSON `APIReply{checks, healthy}` with status **200 if healthy, 503 if not** (`handler.go:57`); the `?tag=` query filters. The same path also accepts JSON-RPC `POST` (`health.readiness/health/liveness`) via `NewGetAndPostHandler` (`handler.go:19`), which forks on `r.Method`.

### Security
- **CORS** (`server.go:327`): `AllowedOrigins` from `--http-allowed-origins` (default `*`), `AllowCredentials: true`.
- **Allowed hosts / DNS-rebinding defense** (`allowed_hosts.go`): the `Host` header is checked against `--http-allowed-hosts` (default `localhost`). A wildcard `*` disables the check. Requests with an empty Host or an IP-literal Host are always allowed; an unlisted hostname gets **403** (`allowed_hosts.go:75`). This blocks DNS-rebinding attacks that bypass CORS.
- **No built-in auth**: there is no token/API-key auth layer in the server. Privileged endpoints are protected by *not exposing them*: the **Admin API is disabled by default** and the HTTP server should be bound to localhost / behind a reverse proxy. Operators restrict access via `--http-host`, allowed hosts, and the per-API enable flags.
- The `node-id` response header (`server.go:334`) is informational, identifying which node served the request.

### Metrics & tracing
- Every wrapped handler increments `calls`, tracks `calls_processing`, and adds to `calls_duration`, labelled by `base` (chain name) (`server/metrics.go`). These live in the API registerer.
- The Prometheus endpoint aggregates a `MultiGatherer` (`metrics/multi_gatherer.go:20`); subsystems register sub-registries namespaced by prefix (`prefix_gatherer.go`) or label (`label_gatherer.go`) via `metrics.MakeAndRegister`.
- If tracing is enabled, `api.TraceHandler` (`traced_handler.go:24`) opens an OTel span per request tagged with method/url/host/etc.

### Indexer invariants
- An index stores containers **by acceptance order**: a monotonic `nextAcceptedIndex` maps `index → Container` and `containerID → index` (`index.go:38`). `Accept` is idempotent — re-accepting an already-stored container is a no-op (`index.go:119`), covering the crash window where the index committed but the VM had not.
- The whole write (both directions + the counter) is committed atomically through a `versiondb` (`index.go:160`).
- Only **primary-network** chains are indexed, and only when `--index-enabled` is set (`indexer.go:142`, `indexer.go:188`).
- DAG VMs (X-Chain pre-linearization) get three indices — `block`, `vtx`, `tx`; linear chains get only `block` (`indexer.go:262`). An unexpected VM type closes the indexer.
- **Incomplete-index guard**: if a chain was indexed in a previous run but indexing is now off (or vice-versa) and `--index-allow-incomplete` is false, the node **fatally exits** to avoid gaps (`indexer.go:189`, `indexer.go:220`).
- `GetContainerRange` is capped at `MaxFetchedByRange = 1024` (`index.go:24`).

### Edge cases
- Adding a route that conflicts with a reserved alias returns `errAlreadyReserved` (`router.go:116`); adding an alias before its base exists still works because routes are mirrored to aliases on creation (`router.go:143`).
- `AddRouteWithReadLock` exists because some callers (e.g. admin's `AliasChain`) register routes while already holding the router read lock during request handling; it temporarily releases it (`server.go:237`).
- A VM whose `CreateHandlers` errors is logged and skipped — the chain still runs without an HTTP surface (`server.go:157`).

---

## 8. Configuration

API enable flags and HTTP options (`config/flags.go`, defaults shown):

| Flag | Default | Effect |
|------|---------|--------|
| `--api-admin-enabled` | `false` | Mount `/ext/admin` (privileged). |
| `--api-info-enabled` | `true` | Mount `/ext/info`. |
| `--api-health-enabled` | `true` | Mount `/ext/health[/...]`. |
| `--api-metrics-enabled` | `true` | Mount `/ext/metrics`. |
| `--index-enabled` | `false` | Run the indexer + mount `/ext/index/...`. |
| `--index-allow-incomplete` | `false` | Permit indices with gaps (otherwise fatal). |
| `--http-host` | `127.0.0.1` | Bind address (empty/unspecified = all interfaces). |
| `--http-port` | `9650` | Port (`0` = auto-assign). |
| `--http-allowed-origins` | `*` | CORS allowed origins. |
| `--http-allowed-hosts` | `localhost` | Allowed `Host` headers; `*` disables the check. |

Per-chain VM endpoints (`/ext/bc/<chainID>`) are always mounted for every running chain; there is no per-chain enable flag at this layer. `HTTPConfig` (read/write/idle timeouts, `server.go:73`) comes from the node's HTTP config block. Config wiring: `config/config.go:311`–`325`; the keys themselves: `config/keys.go`.

There is exactly **one** `http.Server` for the node, constructed at `node.go:1024`. Every endpoint above shares it.

---

## 9. Wallet SDK (high level)

The wallet (`wallet/`) builds and issues P/X/C transactions entirely client-side using a **backend / builder / signer** triad per chain:

- **Builder** (`wallet/chain/*/builder/`) — assembles unsigned transactions, selecting UTXOs and computing fees. It reads available funds from the backend's UTXO view.
- **Signer** (`wallet/chain/*/signer/`) — walks an unsigned tx (visitor pattern, `signer/visitor.go`) and produces credentials from a `keychain.Keychain`, again consulting the backend to resolve which UTXOs/owners a credential must satisfy.
- **Backend** (`wallet/chain/*/backend.go`) — the local, mutable state: a cache of UTXOs (`common.ChainUTXOs`) and (for P-chain) subnet/L1 owners. It is updated optimistically as txs are issued and accepted (`wallet/chain/p/wallet/backend_visitor.go`), so a sequence of dependent txs can be built before any is confirmed.
- **Client** — issues the signed bytes to the node's JSON-RPC endpoint (`IssueTx`).

`pwallet.Wallet` (`wallet/chain/p/wallet/wallet.go:31`) exposes one `Issue<TxType>Tx` method per P-chain transaction (e.g. `IssueAddValidatorTx`, `IssueBaseTx`) that builds → signs → issues in one call, plus accessors `Builder()` / `Signer()` for manual control.

`MakeWallet(ctx, uri, avaxKeychain, ethKeychain, config)` (`wallet/subnet/primary/wallet.go:80`) bootstraps a primary-network wallet: it `FetchState` (UTXOs for the given addresses) and P-chain `GetOwners`, then wires the three per-chain triads and returns a `Wallet` with `.P()`, `.X()`, `.C()` accessors. `WalletConfig.SubnetIDs`/`ValidationIDs` pre-load owners needed for subnet/L1 transactions. Because state is fetched once at construction, an external issuer mutating the same UTXOs can desync the wallet. The `wallet/subnet/primary/examples/` directory contains runnable end-to-end examples for each transaction type.

---

## 10. Cross-References

- [overview.md](overview.md) — where the API server sits in the node.
- [node.md](node.md) — node lifecycle that constructs the server, health, indexer, and registers node-wide APIs.
- [chains.md](chains.md) — `chains.Manager` and the `RegisterChain` fan-out that mounts VM handlers.
- [vm-framework.md](vm-framework.md) — the `common.VM` `CreateHandlers` / `NewHTTPHandler` contract.
- [platformvm.md](platformvm.md), [avm.md](avm.md), [evm.md](evm.md) — per-VM `/ext/bc/<chainID>` endpoint semantics.
- [networking.md](networking.md) — peer/uptime data behind `info.peers` / `info.uptime`.
- [database.md](database.md) — the databases backing the indexer.
- [primitives.md](primitives.md) — IDs, encodings (`formatting.Encoding`), `json.Uint64` used across requests.
- [consensus.md](consensus.md) / [simplex.md](simplex.md) — the acceptance events the indexer subscribes to.
