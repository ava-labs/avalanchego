# Validators

The Validators package is a collection of structs and functions to manage the state and uptime of validators in the Subnet-EVM repository. It consists of the following components:

- State package : The state package stores the validator state and uptime information.
- Uptime package: The uptime package manages the uptime tracking of the validators.
- Manager struct: The manager struct is responsible for managing the state and uptime of the validators.

## State Package

The state package stores the validator state and uptime information. The state package implements a CRUD interface for validators. The implementation tracks validators by their validationIDs and assumes they're unique per node and their validation period. The state implementation also assumes NodeIDs are unique in the tracked set. The state implementation only allows existing validator's `weight` and `IsActive` fields to be updated; all other fields should be constant and if any other field changes, the state manager errors and does not update the validator.

For L1 validators, an `active` status implies the validator balance on the P-Chain is sufficient to cover the continuous validation fee. When an L1 validator balance is depleted, it is marked as `inactive` on the P-Chain and this information is passed to the Subnet-EVM's state.

The state interface allows a listener to register state changes including validator addition, removal, and active status change. The listener always receives the full state when it first subscribes.

The package defines how to serialize the data according to the codec. It can read and write the validator state and uptime information within the database.

## Uptime Package

The uptime package manages the uptime tracking of the L1 validators. It wraps [AvalancheGo's uptime tracking manager](https://pkg.go.dev/github.com/ava-labs/avalanchego/snow/uptime) under the hood and additionally introduces pausable uptime manager interface. The pausable uptime manager interface allows the manager to pause the uptime tracking for a specific validator when it becomes `inactive` and resume it when it becomes `active` again.

The uptime package must be run on at least one L1 node, referred to in this document as the "tracker node".

Uptime tracking works as follows:

### StartTracking

Nodes can start uptime tracking with the `StartTracking` method once they're bootstrapped. This method updates the uptime of up-to-date validators by adding the duration between their last updated time and tracker node's initializing time to their uptime. This effectively adds the tracker node's offline duration to the validator's uptime and optimistically assumes that the validators are online during this period. Subnet-EVM's pausable manager does not directly modify this behavior and it also updates validators that were paused/inactive before the node initialized. The pausable uptime manager assumes peers are online and `active` when the tracker nodes are offline.

### Connected

The AvalancheGo uptime manager records the time when a peer is connected to the tracker node. When a paused/ `inactive` validator is connected, the pausable uptime manager does not directly invoke the `Connected` method on the AvalancheGo uptime manager, thus the connection time is not directly recorded. Instead, the pausable uptime manager waits for the validator to increase its continuous validation fee balance and resume operation. When the validator resumes, the tracker node records the resumed time and starts tracking the uptime of the validator.

Note: The uptime manager does not check if the connected peer is a validator or not. It records the connection time assuming that a non-validator peer can become a validator whilst they're connected to the uptime manager.

### Disconnected

When a peer validator is disconnected, the AvalancheGo uptime manager updates the uptime of the validator by adding the duration between the connection time and the disconnection time to the uptime of the validator. When a validator is paused/`inactive`, the pausable uptime manager handles the `inactive` peers as if they were disconnected. Thus the uptime manager assumes that no paused peers can be disconnected again from the pausable uptime manager.

### Pause

The pausable uptime manager can listen for validator status changes by subscribing to the state. When the state invokes the `OnValidatorStatusChange` method, the pausable uptime manager pauses the uptime tracking of the validator if the validator is currently `inactive`. When a validator is paused, it is treated as if it is disconnected from the tracker node; thus, its uptime is updated from the connection time to the pause time, and uptime manager stops tracking the uptime of the validator.

### Resume

When a paused validator peer resumes, meaning its status becomes `active`, the pausable uptime manager resumes the uptime tracking of the validator. It treats the peer as if it is connected to the tracker node.

Note: The pausable uptime manager holds the set of connected peers that tracks the connected peers in the p2p layer. This set is used to start tracking the uptime of the paused/`inactive` validators when they resume; this is because the AvalancheGo uptime manager thinks that the peer is completely disconnected when it is paused. The pausable uptime manager is able to reconnect them to the inner manager by using this additional connected set.

### CalculateUptime

The `CalculateUptime` method calculates a node's uptime based on its connection status, connected time, and the current time. It first retrieves the node's current uptime and last update time from the state, returning an error if retrieval fails. If tracking hasn’t started, it assumes the node has been online since the last update, adding this duration to its uptime. If the node is not connected and tracking is `active`, uptime remains unchanged and returned. For connected nodes, the method ensures the connection time does not predate the last update to avoid double counting. Finally, it adds the duration since the last connection time to the node's uptime and returns the updated values.

## Manager Struct

`Manager` struct in `validators` package is responsible for managing the state of the validators by fetching the information from P-Chain state (via `GetCurrentValidatorSet` in chain context) and updating the state accordingly. It dispatches a `goroutine` to sync the validator state every 60 seconds. The manager fetches the up-to-date validator set from P-Chain and performs the sync operation. The sync operation first performs removing the validators from the state that are not in the P-Chain validator set. Then it adds new validators and updates the existing validators in the state. This order of operations ensures that the uptimes of validators being removed and re-added under same nodeIDs are updated in the same sync operation despite having different validationIDs.

P-Chain's `GetCurrentValidatorSet` can report both L1 and Subnet validators. Subnet-EVM's uptime manager also tracks both of these validator types. So even if a the Subnet has not yet been converted to an L1, the uptime and validator state tracking is still performed by Subnet-EVM.

Validator Manager persists the state to disk at the end of every sync operation. The VM also persists the validator database when the node is shutting down.