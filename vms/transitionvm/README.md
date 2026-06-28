# TransitionVM

TransitionVM changes a chain's underlying VM as part of a scheduled network
upgrade. No operator action is needed at the upgrade. The operator installs one
binary ahead of time holding both VMs. At the configured `transitionTime` the
node swaps from the old VM to the new one in-process. Peers stay connected and
API endpoints keep working through the swap.

It is a [`block.ChainVM`](../../snow/engine/snowman/block/vm.go) wrapping a
*pre-transition* and a *post-transition* [`Chain`](vm.go). It forwards every call
to whichever is active. Its first use is the C-Chain's migration from **Coreth**
to **SAEVM** at the Helicon upgrade.

The two VMs are not independent. They share one database and one block history,
so they must agree on a database layout and block format. Each must also handle
the other's blocks:

- A node on the pre-transition VM may bootstrap from already-switched peers, so
  the **pre-transition VM must parse post-transition blocks**.
- A switched node still serves history to bootstrapping peers, so the
  **post-transition VM must serve all pre-transition blocks**.

Implementing the [`Chain`](vm.go) interface is necessary but not sufficient.

## Usage

Construct one from a [`Factory`](factory.go) naming the two factories and the
switch time:

```go
n.VMManager.RegisterFactory(context.TODO(), constants.EVMID, &transitionvm.Factory{
    PreFactory:     &coreth.Factory{},
    PostFactory:    &saevm.Factory{},
    TransitionTime: n.Config.UpgradeConfig.HeliconTime.Add(-10 * time.Second),
})
```

The transition is one-way and happens once. The pre-transition VM is then shut
down for good. The switch is recorded durably, so a restart resumes on the right
VM.

## How it works

### One chain, two eras

The transition is a point in the chain's history, not a wall-clock event. It is
anchored to block timestamps and `transitionTime`, so every node draws the
boundary in the same place. Blocks before it belong to the pre-transition VM;
blocks after it belong to the post-transition VM.

```mermaid
flowchart LR
    g(["genesis"]) --> pre["blocks built by the<br/>pre-transition VM"]
    pre -. "transitionTime" .-> post["blocks built by the<br/>post-transition VM"]
    post --> tip(["tip"])

    classDef preCls stroke:#e8820c,stroke-width:2px,color:#e8820c;
    classDef postCls stroke:#2ca02c,stroke-width:2px,color:#2ca02c;
    class pre preCls;
    class post postCls;
```

The pre-transition VM may never extend the chain past this boundary. A block
whose parent already lies in the post-transition era is refused until the node
switches. So the two VMs never disagree about who owns a stretch of history. A
node switches when it accepts the first block at or after `transitionTime`.
Verification and acceptance enforce the boundary:

```mermaid
flowchart TD
    A["Build / Parse / Get block"] --> B["wrap in preBlock"]
    B --> C{"Verify / VerifyWithContext:<br/>maybeTransition"}
    C -->|"parent timestamp &lt; transitionTime"| D["forward Verify to the<br/>pre-transition block"]
    C -->|"parent ≥ transitionTime<br/>and VM not yet transitioned"| E["fail: errPostTransition-<br/>BlockBeforeTransition"]
    C -->|"parent ≥ transitionTime<br/>and VM transitioned"| F["re-parse bytes with the<br/>post-transition chain, then Verify"]
    D --> G{"Accept"}
    F --> G
    G -->|"timestamp &lt; transitionTime<br/>or already transitioned"| H["done"]
    G -->|"timestamp ≥ transitionTime"| I["trigger the transition"]

    classDef preCls stroke:#e8820c,stroke-width:2px;
    classDef postCls stroke:#2ca02c,stroke-width:2px;
    classDef errCls stroke:#d62728,stroke-width:2px;
    class D preCls;
    class F,I postCls;
    class E errCls;
```

### Swapping the VM underneath the node

The consensus engine, the network, and the API server treat a chain's VM as one
long-lived object. TransitionVM keeps that true across the swap. A bare
replacement would drop everything the old VM held, so the wrapper hands that
state to the new VM instead of letting it start cold.

```mermaid
flowchart TB
    ext([Consensus engine · network · API server])
    ext -->|"forwarded calls"| active

    subgraph VM["TransitionVM"]
        active{{"active VM"}}
        pre["pre-transition VM<br/>(Coreth)"]
        post["post-transition VM<br/>(SAEVM)"]
        pre ==>|"hands over consensus state, preference,<br/>peer connections, HTTP routes"| post
    end

    active -.->|"before"| pre
    active -.->|"after"| post

    classDef preCls stroke:#e8820c,stroke-width:2px,color:#e8820c;
    classDef postCls stroke:#2ca02c,stroke-width:2px,color:#2ca02c;
    class pre preCls;
    class post postCls;
```

Three things carry over, each a piece of state the rest of the node assumes is
stable:

- consensus state and block preference (the engine expects them to stick),
- the set of connected peers (the p2p layer won't re-announce them),
- registered HTTP routes (the node mounts these at startup).

Beyond this surface the two VMs are isolated.

### Requests across the swap

One piece of state must *not* carry over: in-flight app requests. Handing a VM a
response to a request it never sent is fatal to the consensus engine. After the
swap, the new VM knows nothing of the old VM's outstanding requests.

```mermaid
sequenceDiagram
    participant Peer
    participant VM as TransitionVM
    participant Pre as pre-transition VM
    participant Post as post-transition VM

    Pre->>VM: SendAppRequest (VM records it)
    VM->>Peer: AppRequest
    Note over VM,Post: transition: pre shut down, post now active
    Peer->>VM: AppResponse
    VM--xPost: dropped: post never sent this request
```

So TransitionVM tracks the requests each VM sends. It drops any response or
failure that doesn't match one the active VM made, including late replies to the
shut-down VM's requests.

## Concurrency

The transition replaces the active VM while other goroutines forward calls
through the wrapper. The swap must be atomic against all of them. A single
`transitionLock` provides this: forwarded calls hold it as readers, the
transition holds it as the writer. No caller sees a half-swapped VM, and the swap
waits for in-flight calls to drain.

That alone would deadlock, because one forwarded call blocks on purpose.
`WaitForEvent` parks until the VM has work, holding the read lock the whole time,
and a VM is idle most of the time. A writer asking for the lock would wait
forever behind it. The transition avoids this by cancelling the active VM's
context before taking the writer lock. The cancellation wakes `WaitForEvent`, it
returns and releases the read lock, and the swap proceeds.

`Accept` adds a wrinkle: it triggers the transition but is itself a forwarded
call. It cannot hold the read lock, or it would deadlock against the writer lock
it is about to request. Instead it relies on the wrapped block being immutable
once verified.

These tensions shape the locking. The line-level mechanics are commented at their
call sites in [`vm.go`](vm.go) and [`vm_block.go`](vm_block.go).
