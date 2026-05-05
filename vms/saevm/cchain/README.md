# C-Chain VM (`cchain`)

`cchain` is the C-Chain VM. It is a thin chain-specific harness around [saevm](../), the generic EVM framework that implements [ACP-194](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-streaming-asynchronous-execution). `saevm` does the heavy lifting — block execution, settlement, gas accounting, and EVM gossip. `cchain` adds what makes the chain *the C-Chain*: Transactions for moving assets between Primary Network chains, warp messaging, validator voting of chain parameters, and minimum block delay enforcement.

## Architecture

The C-Chain is composed of three major components:

1. **AvalancheGo** — host infrastructure: networking, consensus, validator-set management, and the external API surface.
2. **SAE** — the generic, Turing-complete EVM implementation.
3. **C-Chain** (this package) — the wrapper that adds C-Chain-specific behavior.

```mermaid
flowchart TB
    subgraph avago["AvalancheGo"]
        direction LR
        consensus["Consensus"]
        network["Network"]
        apiserver["API Server"]
    end

    subgraph sae["SAE"]
        direction LR
        execution["Execution"]
        p2pn["P2P Network"]
        rpc["/rpc"]
        ws["/ws"]
    end

    subgraph cchain["C-Chain"]
        direction LR
        hook["Hooks"]
        warp["Warp"]
        db[("Database")]
        txpool[("Txpool")]
        avax["/avax"]
    end

    network <--> consensus
    network --> p2pn
    consensus --> execution
    apiserver --> rpc
    apiserver --> ws
    apiserver --> avax
    execution --> hook

    hook --> warp
    p2pn --> warp

    hook --> txpool
    p2pn --> txpool
    avax --> txpool

    hook --> db
    warp --> db
    txpool --> db
```

## What `cchain` adds

`cchain` layers four chain-specific behaviors on top of SAE: Import/Export transactions for cross-chain transfers, warp messaging, and two chain parameters that validators vote on each block.

### Export and Import transactions

The Primary Network is the set of three chains (P, X, and C) that transfer assets through pair-wise shared stores: each pair of chains shares its own store that both chains can read and write.

```mermaid
flowchart TB
    P((P-Chain))
    X((X-Chain))
    C((C-Chain))

    PX[("PX")]
    CP[("CP")]
    CX[("CX")]

    P --> PX
    X --> PX
    P --> CP
    C --> CP
    X --> CX
    C --> CX
```

A cross-chain transfer happens in two steps. An **Export** transaction on the source chain burns the asset there and writes a corresponding entry into the shared store between the source and destination chains. The destination chain later picks up that entry with an **Import** transaction, consuming it and crediting the recipient account.

`cchain` defines both transaction types and their validation rules, runs a dedicated mempool keyed on the entries each transaction consumes, and operates a bloom-filter gossip system for them. See [tx](tx/) and [txpool](txpool/).

The mempool does not support dependent transactions: every transaction it holds must be valid on its own against the current chain state. Two invariants follow. First, the mempool is always valid against a recently-executed state, so anything it offers up for block building can be applied directly. Second, if block production stalls, all mempool transactions are eventually guaranteed to be valid against the last accepted block.

### Warp messaging

The C-Chain participates in cross-subnet warp messaging on both sides — sending messages to other chains and receiving messages from them. Three pieces are involved:

- A custom precompile that lets EVM contracts emit and consume warp messages.
- An encoding that places warp message payloads into transaction access lists, so the message rides alongside the transaction that produced or accepted it.
- The [ACP-118](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/118) p2p protocol for collecting BLS signatures from peer validators on outbound messages.

`cchain` persists this chain's warp messages, serves signature requests against that store, and verifies warp predicates during block execution. See [warp](warp/).

### Validator-voted gas target (ACP-176)

Most of [ACP-176](https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/176) — the gas accounting and excess tracker — sits inside SAE. What `cchain` adds is the *target* itself: a target gas-per-second that validators vote on directly. Each block, validators choose to raise, lower, or hold the target. Throughput is set by validator agreement on a sustainable rate, not auto-discovered from observed usage. See [hook/acp176](hook/acp176/).

### Validator-voted minimum block delay (ACP-226)

A lower bound on the time between consecutive blocks, derived from the parent header. Like the gas target, this is validator-voted: each block, validators raise, lower, or hold the bound. The mechanism prevents block production faster than the network has agreed to. See [hook](hook/).

## How transactions enter the Txpool

Import/Export transactions can reach the Txpool from four independent sources, but every source ends at the same call into the mempool's add path. The mempool is the single write-side gate: signature checks, against-state checks, and conflict resolution all happen there.

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
