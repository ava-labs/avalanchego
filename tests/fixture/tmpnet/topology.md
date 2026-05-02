# Tmpnet topology

This document records the current behavior and durable feature context for
tmpnet topology support.

- **For users**: what topology does, when to use it, and its current limits
- **For maintainers**: why the feature is shaped this way today, what
  constraints matter, and what should trigger reconsideration later

`README.md` remains the concise package reference. This file is the deeper,
feature-specific explanation.

## Overview

Tmpnet's default behavior assumes a largely colocated network. Topology support
exists for tests that need a declared, persistent connectivity layout between
groups of nodes rather than uniform local behavior.

Today, tmpnet realizes topology only for kube-backed networks, using Chaos Mesh
`NetworkChaos` resources to induce persistent one-way latency between declared
locations.

Topology is modeled as a property of the **network**, not of an individual
node, because it describes relationships between sets of nodes.

---

## For users

### What topology does

A topology lets a tmpnet network declare:

- named **locations** containing one or more nodes
- directed **connections** between locations
- a persistent latency value for each directed connection
- whether topology is **required** or **best-effort** when the runtime cannot
  realize it

Each connection is directional. To model symmetric latency between two
locations, define one connection in each direction.

### When to use it

Use topology when a test needs connectivity characteristics different from the
default colocated behavior. Typical uses include:

- modeling geographically separated groups of nodes
- validating behavior under persistent latency between node groups
- exercising kube-backed tmpnet workflows with network impairment

### Current support and limits

Current support is intentionally narrow:

- topology is only realized for **kube-backed** networks
- the realized impairment is currently **persistent latency**
- unsupported runtimes can either fail (`required`) or warn and continue
  (`best-effort`)
- topology is treated as fixed for the lifetime of a network

Current non-goals:

- dynamic topology mutation after network start
- adding new locations to a running network
- non-kube realization of topology
- a general framework for every possible network fault mode

### Configuration model

Topology is configured on `tmpnet.Network` and persisted as part of the
network-level configuration on disk.

Topology is expressed through:

- `Topology.Mode`
- `Topology.Locations`
- `Topology.Connectivity`

`Mode` values:

- `required` - fail if topology is configured but cannot be realized
- `best-effort` - warn and continue if the runtime is unsupported

A location groups nodes by a human-meaningful name. A connection specifies:

- `From`
- `To`
- `Latency`

Example:

```go
nodes := tmpnet.NewNodesOrPanic(2)
network := &tmpnet.Network{
    Nodes: nodes,
    Topology: &tmpnet.Topology{
        Mode: tmpnet.TopologyModeRequired,
        Locations: []tmpnet.Location{
            {Name: "chicago", NodeIDs: []ids.NodeID{nodes[0].NodeID}},
            {Name: "new-york", NodeIDs: []ids.NodeID{nodes[1].NodeID}},
        },
        Connectivity: []tmpnet.Connection{
            {From: "chicago", To: "new-york", Latency: "10ms"},
            {From: "new-york", To: "chicago", Latency: "10ms"},
        },
    },
}
```

This requests 10ms of additional one-way latency in each direction between the
two locations.

### Lifecycle behavior

For kube-backed networks, tmpnet currently:

1. starts the relevant node pods
2. applies topology before the final healthy wait completes
3. removes topology resources during `Network.Stop`

In other words, topology is treated as part of the network's steady-state
execution environment, not as a separate post-start test step.

### Runtime and cluster requirements

For kube-backed realization to work:

- Chaos Mesh must be installed in the target cluster
- tmpnet must have RBAC permission to manage `chaos-mesh.org`
  `NetworkChaos` resources
- all nodes in the network must use the kube runtime

If kube is selected but topology application fails for a concrete reason such as
missing RBAC or missing Chaos Mesh resources, tmpnet treats that as an error.
`best-effort` only softens the case where topology is configured but the runtime
itself is unsupported.

---

## For maintainers

### Design overview

At a high level:

- topology is declared at network scope
- kube-specific realization is performed as a network operation
- kube targeting uses tmpnet-owned labels rather than fixed pod names
- one `NetworkChaos` resource is created per directed connection

Most of the feature logic lives in `network.go`, `topology_runtime.go`,
`node.go`, and the topology end-to-end test.

### Current design rationale

#### Why topology is modeled at network scope

Topology describes relationships between groups of nodes, not an intrinsic
property of any one node. Modeling it on the network keeps the user-facing API
closer to the concept being expressed: "this network has these locations and
these connectivity relationships between them".

A node-centric model was possible, but it would have made directional
connectivity harder to express coherently and would have obscured the fact that
multiple nodes can share the same location identity.

#### Why realization is kube-specific today

The current implementation relies on Chaos Mesh `NetworkChaos`, which already
provides a practical way to induce persistent delay for kube-managed pods.
Tmpnet does not currently have an equivalent abstraction for process-backed
nodes or other environments.

The feature therefore starts with a narrow, working backend instead of a
speculative cross-runtime abstraction.

#### Why label-based targeting was chosen

Realization targets pods by:

- `network_uuid`
- `tmpnet.avax.network/location=<location>`

rather than by enumerating specific pod names.

The declared topology is about location-to-location connectivity, not a frozen
set of pod instances. Label-based targeting keeps the realized kube resources
aligned with the declared model and makes ownership easier to reason about.

#### Why realized connections have deterministic names

Each directed connection maps to a deterministic tmpnet-owned resource name
based on the network UUID and directed connection identity.

This gives tmpnet unambiguous ownership and lets tests or operators correlate a
connection with its realized resource without depending on list order or pod
instance names.

#### Why `required` vs `best-effort` exists

There are two distinct failure classes:

1. topology is configured, but the runtime cannot realize topology at all
2. topology should be realizable in the selected runtime, but application fails
   in practice

`best-effort` exists only for the first case. It allows a caller to say "apply
this when possible, but do not fail just because this environment is not
kube-backed".

It does **not** downgrade concrete kube realization failures such as missing
Chaos Mesh installation or missing RBAC. Those remain errors because silently
continuing would hide a misconfigured test environment.

#### Why topology is integrated into network lifecycle

Topology is not implemented as an ad hoc post-start helper. The intent is for
the network to *be* in that topology while it becomes ready for use.

In the current implementation, kube resources are applied after node pods exist
and before the final healthy wait completes. Removal happens during
`Network.Stop`.

That reflects the current model: topology is part of the network's intended
steady-state execution environment.

### Alternatives considered

#### Node-centric declaration

A plausible alternative was to attach topology metadata directly to nodes.
That would make per-node local data easy to inspect, but it weakens the
representation of directional connectivity between groups and makes shared
location identity less explicit.

#### Pod-name targeting

Another option was to realize connections by targeting explicit pod names.
That is more direct in the moment, but it ties realized resources to concrete
pod instances instead of the declared topology model.

#### Post-start application only

Topology could have been applied only after the network had already converged to
healthy. That would make the feature feel more like a separate test step than a
network property, and it would reduce confidence that the network was healthy
under the configured topology rather than only before it.

#### Generic cross-runtime abstraction first

Tmpnet could have tried to define a runtime-independent topology realization
layer before implementing any concrete backend. That would likely have been more
abstract, but also more speculative. The current feature instead starts with a
working kube realization and leaves broader abstraction for a later need.

### Constraints shaping the current implementation

Non-obvious constraints that shaped the design:

- **Chaos Mesh dependency**: realization is coupled to the Chaos Mesh API used by
  tmpnet's kube workflows
- **RBAC reality**: kube-based realization and privileged verification are not
  necessarily performed with the same permissions
- **Lifecycle semantics**: `Stop` matters for topology cleanup, but is not
  necessarily the same as full cluster-resource destruction
- **Validation cost**: unit tests alone are insufficient to prove kube
  realization
- **Flake sensitivity**: induced latency must dominate baseline noise without
  making the test too brittle

### Validation strategy

Validation is intentionally layered.

#### Unit / focused tests

`topology_runtime_test.go` covers:

- topology object construction
- duplicate connection rejection
- duplicate node membership rejection
- mode semantics
- applicability behavior for kube vs process runtime
- warning behavior for `best-effort`
- resource-injected status interpretation

These tests guard local semantics and topology applicability logic.

#### End-to-end test

`e2e/e2e_test.go` validates the kube realization path on kind by:

- bootstrapping a kube-backed tmpnet network with topology configured
- confirming expected Chaos Mesh resources are created
- checking location labels on pods
- measuring an induced latency delta large enough to distinguish the affected
  path from baseline noise
- stopping the network
- confirming tmpnet-owned topology resources are removed on teardown

This test exists because the core risk is not only bad local logic; it is
"tmpnet believes it realized topology but kube did not actually apply the
intended behavior".

This validation is also wired into a dedicated topology-oriented path in CI so
that kube realization is exercised separately from the focused unit tests.

#### Runtime vs admin kube contexts in e2e

The end-to-end path distinguishes:

- the runtime kube context tmpnet uses to exercise the feature itself
- an admin/verification kube context used by the test to inspect resources and
  exec into pods

That split is intentional: the test is both exercising tmpnet's expected
permissions and performing extra verification that may require broader access.

### Invariants and maintenance notes

Preserve these invariants unless the design is being reconsidered deliberately:

- topology remains a network-level declaration of relationships between
  locations
- location labels remain consistent with topology location membership
- managed topology resources remain clearly tmpnet-owned and removable by
  network identity
- unsupported runtime behavior remains distinct from concrete realization
  failure in a supported runtime
- teardown should not silently regress into leaking tmpnet-owned topology
  resources

### Revisit if assumptions change

The current design should be reconsidered if any of these become true:

- tmpnet gains an explicit destroy/delete lifecycle distinct from `Stop`
- non-kube runtimes need real topology support rather than a warning/fail split
- topology mutation after startup becomes a real product requirement
- kube RBAC expectations change enough that separate runtime and verification
  contexts are no longer needed
- a cleaner in-cluster Go-based latency measurement approach becomes preferable
  to the current shell-based e2e helper
- topology broadens from persistent latency into a more general fault-injection
  model

### Open questions

Durable questions worth revisiting:

- should topology validation eventually accumulate and report multiple user
  errors rather than stopping at the first one?
- should topology testing remain a dedicated lane or move into a broader
  robustness strategy?
- should kube-specific seams be pushed lower in the abstraction stack if tmpnet
  gains more runtime-specific features?

## References

Relevant code and tests:

- `tests/fixture/tmpnet/network.go`
- `tests/fixture/tmpnet/node.go`
- `tests/fixture/tmpnet/topology_runtime.go`
- `tests/fixture/tmpnet/topology_runtime_test.go`
- `tests/fixture/tmpnet/e2e/e2e_test.go`
- `tests/fixture/tmpnet/flags/kubeconfig.go`
- `scripts/tests.topology.kube.kind.sh`

Related package documentation:

- `tests/fixture/tmpnet/README.md`
