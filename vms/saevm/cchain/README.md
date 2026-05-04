# C-Chain VM (`cchain`)

`cchain` is the C-Chain VM. It is a thin chain-specific harness wrapped around [saevm](../), the generic streaming-asynchronous EVM framework that implements [ACP-194](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-streaming-asynchronous-execution). `saevm` does the heavy lifting — block execution, settlement, gas accounting, EVM gossip, and the Snowman adaptor. `cchain` adds what makes the chain *the C-Chain*: Import/Export transactions for moving assets between Primary Network chains, warp signatures, dynamic gas targeting, minimum-delay block production, and the migration boundary from the chain's historical synchronous era.

## Where `cchain` sits

```mermaid
flowchart TB
    node["AvalancheGo node<br/>(Snowman consensus engine)"]

    subgraph saevm_grp["saevm"]
        adaptor["adaptor<br/><i>generic VM &harr; Snowman</i>"]
        sae["inner saevm VM<br/><i>execution, gossip, p2p</i>"]
        hooks["hook<br/><i>extension points</i>"]
    end

    subgraph cchain_grp["cchain (this package)"]
        cvm["cchain VM harness"]
        chook["cchain hook<br/><i>fills extension points</i>"]
        cextras["txpool &middot; api &middot; warp &middot; tx &middot; state"]
    end

    libevm["libevm + ethdb"]

    node --> adaptor
    adaptor --> cvm
    cvm --> sae
    cvm --- cextras
    chook -. implements .-> hooks
    sae --> libevm
```

`cchain` wraps an inner saevm VM. Block execution, settlement, persistence, gas accounting, and the EVM transaction gossip plumbing all run inside saevm. saevm exposes a hook surface — a set of extension points where chain-specific logic can be injected — and `cchain` fills those slots. Outside the hook seam, `cchain` adds two things: a custom mempool and JSON-RPC service for Import/Export transactions, and a warp signature-request handler. Both share saevm's network and lifecycle.

## What `cchain` adds

The Primary Network is the set of three chains (P, X, and C) that exchange assets through a per-chain shared key-value store. Each chain has a slot, and chains hand assets to each other by writing to the recipient's slot.

### Import / Export transactions
Transfers between the C-Chain and the other Primary Network chains, via that shared store. **Import** transactions consume entries another chain wrote into the C-Chain's slot and credit the matching C-Chain accounts; **Export** transactions burn C-Chain balances and write the matching entries into the destination chain's slot. `cchain` defines the transaction types and validation rules, runs a dedicated mempool keyed on consumed inputs, and operates a bloom-filter gossip system for them. See [tx](tx/) and [txpool](txpool/).

### Warp signature service
Cross-subnet signed messages following [ACP-118](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/118). When the chain emits a warp message, `cchain` stores it; when peers ask this node to sign a message they have, `cchain` verifies the request against the stored set and (for accepted messages) returns a BLS signature. See [warp](warp/).

### Dynamic gas target (ACP-176)
The target gas-per-second is not a fixed parameter. It follows an excess tracker that adjusts up or down based on observed usage, letting the network discover a sustainable throughput rate from the bottom up. See [hook/acp176](hook/acp176/).

### Minimum block delay (ACP-226)
A configurable lower bound on the time between consecutive blocks, derived from the parent header. The bound prevents accelerated block production beyond what the network has agreed to. See [hook](hook/).

### Synchronous-to-asynchronous migration
The C-Chain executed synchronously for years before streaming-asynchronous execution was introduced. `cchain` records the boundary block at which the chain switched modes, so a node bootstrapping from genesis correctly replays the synchronous era and then hands off to saevm's asynchronous pipeline for everything after. See [state](state/) and [hook](hook/).

## How `cchain` shares the network with saevm

`cchain` does not create its own peer-to-peer network. saevm builds one — an AvalancheGo p2p Network, plus a peer set and a validator-only peer set — during construction of the inner VM. `cchain` embeds the inner VM and reaches through it: it asks saevm to register two handlers, each keyed by a stable handler ID.

| Handler ID | Owner of the registered handler | Purpose |
| --- | --- | --- |
| EVM transaction gossip | saevm | Gossip of standard EVM transactions for the saevm mempool |
| Import/Export gossip | `cchain` | Gossip of Import/Export transactions for the [txpool](txpool/) |
| Warp signature requests | `cchain` | ACP-118 signature requests against [warp](warp/) storage |

Inbound peer messages flow through one shared dispatch path. AvalancheGo's chain router delivers an app message to the VM, the VM forwards it into saevm's p2p Network, and the Network looks up the handler by ID:

```mermaid
sequenceDiagram
    participant Peer
    participant Router as AvalancheGo router
    participant Net as saevm p2p Network<br/>(owned by the inner saevm VM)
    participant Disp as Handler dispatch<br/>(by handler ID)
    participant Gossip as cchain gossip handler
    participant Pool as cchain mempool
    participant Warp as cchain warp handler
    participant Storage as warp storage

    Peer->>Router: AppGossip / AppRequest
    Router->>Net: forward to chain VM
    Net->>Disp: route by handler ID

    alt Import/Export tx gossip
        Disp->>Gossip: deliver gossip payload
        Gossip->>Gossip: unmarshal via cchain's marshaller
        Gossip->>Pool: add to mempool (write-side path)
    else Warp signature request
        Disp->>Warp: deliver signature request
        Warp->>Storage: look up message
        Warp-->>Peer: BLS signature
    end
```

The two `cchain` handlers are co-resident on the same Network instance saevm uses for its own EVM-tx gossip, so peer accounting, throttling, and metrics are shared. saevm owns the Network's lifecycle; the `cchain` handlers register on it during initialization and are unregistered implicitly when saevm shuts the Network down.

## How transactions enter the mempool

Import/Export transactions can reach the mempool from four independent sources, but every source ends at the same call into the mempool's add path. The mempool is the single write-side gate: signature checks, against-state checks, and conflict resolution all happen there.

```mermaid
flowchart LR
    rpc["User RPC<br/>(/avax submit endpoint)"]
    push["Inbound push gossip<br/>(peer-initiated)"]
    pull["Inbound pull gossip<br/>(periodic, this node pulls)"]
    rej["Block rejection<br/>(consensus rejects a block we built/verified)"]

    subgraph cchain_write["cchain mempool (write side)"]
        add(("mempool add"))
    end

    subgraph downstream["downstream"]
        out["push gossiper queue<br/>(outbound propagation)"]
        store["shared tx store<br/>(read by hook for block building)"]
    end

    rpc -->|decode bytes| add
    rpc -. on success / already known .-> out
    push -->|via bloom set + cchain marshaller| add
    pull -->|via bloom set + cchain marshaller| add
    rej -->|extract embedded txs from rejected block| add

    add -->|verify, dedup, evict, insert| store
```

The four entry paths in detail:

- **User RPC submission.** The HTTP `/avax` endpoint decodes a transaction from request bytes and submits it. On a successful add (or on "already known"), the same call also enqueues the transaction onto the local push-gossiper so this node will propagate it.
- **Inbound push gossip.** A peer pushes a transaction over the Import/Export gossip protocol. saevm's network dispatches it to `cchain`'s registered gossip handler, the gossip system unmarshals using `cchain`'s gossip marshaller, and a bloom-set wrapper around the mempool routes the decoded transaction to the same add path.
- **Inbound pull gossip.** A periodic goroutine inside `cchain` pulls digests from peers; transactions returned in response flow into the same bloom-set wrapper and the same add path.
- **Block rejection.** When the consensus engine rejects a block this node had previously verified, `cchain` extracts the Import/Export transactions embedded in the block's extension data and re-submits each one to the mempool. The point is to keep otherwise-valid transactions from being dropped by an unlucky reorg.

### Write side versus read side — the shared tx store

The user-facing mempool wraps a smaller heap-sorted store. The store is shared.

```mermaid
flowchart LR
    subgraph cchain_pkg["cchain"]
        write["mempool (write side)<br/><i>signature + state verification,<br/>height tracking, conflict eviction</i>"]
        store["shared tx store<br/><i>min-heap by gas price,<br/>conflict map, condition var</i>"]
        hookimpl["cchain hook (read side)<br/><i>iterates store for block-building candidates</i>"]
    end
    subgraph saevm_pkg["saevm"]
        bb["block builder"]
    end

    write -->|all four entry paths<br/>insert/evict| store
    hookimpl -->|gas-price-ordered iteration| store
    bb -->|asks hook for end-of-block candidates| hookimpl
```

The store is purely mechanical — sorting, deduplication by ID, conflict tracking by consumed input, and a condition variable that signals waiters when something is added. The mempool layered on top adds the policy: it tracks the latest accepted height, drops conflicts when a new block lands, and runs sanity, signature, shared-state, and current-state checks before any insert.

Block building goes the other way. saevm's block builder asks the `cchain` hook for end-of-block candidates; the hook iterates the same store in decreasing gas-price order. Construction of the hook receives the store directly — the hook never goes through the mempool — so reads are cheap and never contend with the mempool's verification logic.

## Ownership and lifecycle

The `cchain` VM struct holds the inner saevm VM, a mempool, a push-gossiper handle, a hook reference, and a list of cleanup callbacks. Its construction order matters because several components are shared by reference between consumers, and shared things must exist before either consumer is built.

```mermaid
flowchart TB
    subgraph shared["shared instances"]
        txsstore["shared tx store"]
        warpstore["warp storage"]
        hookpoints["cchain hook"]
    end

    subgraph cchainvm["cchain VM struct"]
        cvm["VM"]
        mempool["mempool"]
        pushg["push gossiper handle"]
        onclose["onClose cleanup list"]
    end

    subgraph saeinternals["saevm internals"]
        innersae["inner saevm VM"]
        net["p2p Network"]
        peers["peer / validator-peer sets"]
    end

    cvm ==>|creates| txsstore
    cvm ==>|creates| warpstore
    cvm ==>|creates| hookpoints
    cvm ==>|creates| innersae
    cvm ==>|creates| mempool

    hookpoints -. holds .-> txsstore
    hookpoints -. holds .-> warpstore
    mempool -. wraps .-> txsstore
    innersae -. holds .-> hookpoints
    innersae ==>|creates and owns| net
    innersae ==>|creates and owns| peers

    cvm -. registers handlers on .-> net
    cvm -. holds .-> innersae
    cvm -. holds .-> hookpoints
    cvm -. holds .-> mempool
    cvm -. holds .-> pushg
```

Solid arrows are *creates*; dashed arrows are *holds a reference to*. The shared boxes — store, warp storage, hook — each have one creator and two holders. The store is created before the hook and the mempool, and is then handed to both. The warp storage is created before the hook and the warp signature verifier, and is handed to both. The hook is created before the inner saevm VM, and is held by both the inner VM (which calls into it during block execution) and the `cchain` VM struct.

Once everything is built, gossip is wired: a bloom-set wrapper, a push gossiper, a pull gossiper, and a network handler are all produced together and the handler is registered on the inner VM's Network under the Import/Export handler ID. A separate warp signature handler is registered under the warp handler ID. Two long-running goroutines drive periodic push and pull gossip; their cancel function and a wait group are added to the cleanup list.

### Shutdown

Shutdown unwinds in reverse order. `cchain` runs its cleanup callbacks last-in-first-out, then asks the inner saevm VM to shut down. The order matters because the resources `cchain` cleans up hold references *into* the inner VM:

- The gossip goroutines pull from peers via the inner VM's Network. If saevm tore the Network down first, those goroutines would be reading dead state.
- The mempool subscribes to chain-head events from the inner VM's RPC backend. The subscription must be unsubscribed before the inner VM finishes its own shutdown.

The hook, the warp storage, and the shared tx store carry no goroutines or external subscriptions of their own. They live and die with the process and need no explicit cleanup; releasing the references is enough.

## Subpackages at a glance

- [api/](api/) — `avax_*` JSON-RPC service for Import/Export submission, status, and UTXO lookup
- [hook/](hook/) — implementation of saevm's hook surface; orchestrates header construction, end-of-block operations, and per-block timing
- [hook/acp176/](hook/acp176/) — dynamic gas-target excess tracker
- [state/](state/) — genesis parsing, the synchronous-boundary pointer, and state-trie helpers
- [tx/](tx/) — Import / Export transaction types and their gossip marshaller
- [txpool/](txpool/) — the shared store, the mempool that wraps it, and conflict tracking
- [warp/](warp/) — warp message storage, the ACP-118 verifier, and predicate handling
