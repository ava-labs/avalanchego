# Shared Memory

Shared memory creates a way for blockchains in the Avalanche Ecosystem to communicate with each other by using a shared database to create a bidirectional communication channel between any two blockchains in the same subnet.

## Shared Database

### Using Shared Base Database

AvalancheGo uses a single base database (typically leveldb). This database is partitioned using the `prefixdb` package, so that each recipient of a `prefixdb` can treat it as if it were its own unique database.

Each blockchain in the Avalanche Ecosystem has its own `prefixdb` passed in via `vm.Initialize(...)`, which means that the `prefixdb`s that are given to each blockchain share the same base level database.

Shared Memory, which also uses the same underlying database, leverages this fact to support the ability to combine a database batch performed on shared memory with any other batches that are performed on the same underlying database.

This allows VMs the ability to perform some database operations on their own database, and commit them atomically with operations that need to be applied to shared memory.

### Creating a Unique Shared Database

Shared Memory creates a unique sharedID for any pair of blockchains by ordering the two blockchainIDs, marshalling it to a byte array, and taking a hash of the resulting byte array.

This sharedID is used to create a `prefixdb` on top of shared memory's database. The sharedID prefixed database is the shared database used to create a bidirectional communication channel.

The shared database is split up into two `state` objects one for each blockchain in the pair.

Shared Memory can then build the interface to send a message from ChainA to ChainB, which can then only be read and deleted by ChainB. Specifically, Shared Memory exposes the following interface:

```go
type SharedMemory interface {
    // Get fetches the values corresponding to [keys] that have been sent from
    // [peerChainID]
    Get(peerChainID ids.ID, keys [][]byte) (values [][]byte, err error)
    // Indexed returns a paginated result of values that possess any of the
    // given traits and were sent from [peerChainID].
    Indexed(
        peerChainID ids.ID,
        traits [][]byte,
        startTrait,
        startKey []byte,
        limit int,
    ) (
        values [][]byte,
        lastTrait,
        lastKey []byte,
        err error,
    )
    // Apply performs the requested set of operations by atomically applying
    // [requests] to their respective chainID keys in the map along with the
    // batches on the underlying DB.
    //
    // Invariant: The underlying database of [batches] must be the same as the
    //            underlying database for SharedMemory.
    Apply(requests map[ids.ID]*Requests, batches ...database.Batch) error
}
```

When ChainA calls `Apply`, the `requests` map keys are the chainIDs on which to perform requests. If ChainA wants to send a Put and Remove request on ChainB, then it will be broken down as follows:

Shared Memory grabs the shared database between ChainA and ChainB and it will first perform the Remove request. ChainA can only remove messages that were sent from ChainB, so the Remove request will be processed by Removing the specified message from ChainA's state.

The Put operation needs to be sent to ChainB, so any Put request will be added to the state of ChainB.

`Get` and `Indexed` will both grab the same shared database and then look at the state of ChainA to read the messages that have been sent to ChainA from ChainB.

This setup ensures that the SharedMemory interface passed in to each VM can only send messages to destination chains and can only read and remove messages that were delivered to it by another source chain.

### Atomic Elements

Atomic Elements contain a Key, Value, and a list of traits, all of which are byte arrays:

```go
type Element struct {
    Key    []byte   `serialize:"true"`
    Value  []byte   `serialize:"true"`
    Traits [][]byte `serialize:"true"`
}
```

A Put operation on shared memory contains a list of Elements to be sent to the destination chain. Each Element is then indexed into the state of the recipient chain.

The Shared Memory State is divided into a value database and index database as mentioned above. The value database contains a one-to-one mapping of `Key -> Value`, to support efficient `Get` requests for the keys. The index database contains a one-to-many mapping from a `Trait` to the `Keys` that possess that `Trait`.

Therefore, a Put operation performs the following actions to maintain these mappings:

- Add `Key -> Value` to the value database
- Add `Trait -> Key` for each Trait in the element to the one-to-many mapping in the index database

### Accessing the Shared Database

Shared Memory creates a shared resource across multiple chains which operate in parallel. This means that Shared Memory must provide concurrency control. Therefore, when grabbing a shared memory database, we use the functions `GetSharedDatabase` and `ReleaseSharedDatabase`, which must be called in conjunction with each other.

Under the hood, memory creates a shared lock when `makeLock(sharedID)` is called. This returns the lock without grabbing it and tracks the number of callers that have requested access to the shared database. After `makeLock(sharedID)` is called, `releaseLock(sharedID)` is called in `memory.go` to return the same lock and decrement the count of callers that have access to it.

`Lock()` and `Unlock()` are not called within `makeLock(sharedID)` and `releaseLock(sharedID)`, it's up to the caller of these functions to grab and release the returned lock.

Returning the lock instead of grabbing it within the function, ensures that only the thread calling `GetSharedDatabase` will block. We grab the lock outside of `makeLock` to avoid grabbing a shared lock while holding onto the lock within `memory.go`, which allows access to the maintained maps of shared locks.

## Using Shared Memory for Cross-Chain Communication

Shared Memory enables generic cross-chain communication. Here we'll go through the lifecycle of a message through shared memory that is used to move assets from ChainA to ChainB.

### Issue an export transaction to ChainA

ChainA will verify this transaction is valid within the block containing the transaction. This verification will ensure that it pays an appropriate fee, ensure that the transaction is well formed, and check to ensure that the destination chain, ChainB, is on the same subnet as ChainA. After the block containing this transaction has been verified, it will be issued to consensus. It's important to note that the message to the shared memory of ChainB is added when this transaction is accepted by the VM. This ensures that an import transaction on ChainB is only valid when the atomic UTXO has been finalized on ChainA.

### API service uses Indexed to return the set of UTXOs owned by a set of addresses

A user that wants to issue an import transaction may need to look up the UTXOs that they control. Atomic UTXOs use the traits field to include the set of addresses that own them. This allows a VM's API service to use the `Indexed` call to look up all of the UTXOs owned by a set of addresses and return them to the user. The user can then form an import transaction that spends the given atomic UTXOs.

### Issue an import transaction to ChainB

The user issues an import transaction to ChainB, which specifies the atomic UTXOs that it spends from ChainA. This transaction is verified within the block that it was issued in. ChainB will check basic correctness of the transaction as well as confirming that the atomic UTXOs that it needs to spend are valid. This check will contain at least the following checks:

- Confirm the UTXO is present in shared memory from the `sourceChain`
- Confirm that no blocks in processing between the block containing this tx and the last accepted block attempt to spend the same UTXO
- Confirm that the `sourceChain` is an eligible source, which means at least that it is on the same subnet

Once the block is accepted, then the atomic UTXOs spent by the transaction will be consumed and removed from shared memory. It's important to note that because we remove UTXOs from shared memory when the transaction is accepted, we need to verify that any processing ancestor of the block we're verifying does not conflict with the atomic UTXOs that are being spent by this block. It is not sufficient to check that the atomic UTXO we want to spend is present in shared memory when the block is verified because there may be another block that has not yet been accepted, which attempts to spend the same atomic UTXO.

For example, there could be a chain of blocks that looks like the following:

```text
L    (last accepted block)
|
B1   (spends atomic UTXOA)
|
B2   (spends atomic UTXOA)
```

If B1 is processing (has been verified and issued to consensus, but not accepted yet) when block B2 is verified, then ChainB may look at shared memory and see that `UTXOA` is present in shared memory. However, because its parent also attempts to spend it, block B2 obviously conflicts with B1 and is invalid.

## Generic Communication

Shared memory provides the interface for generic communication across blockchains on the same subnet. Cross-chain transactions moving assets between chains is just the first example. The same primitive can be used to send generic messages between blockchains on top of shared memory, but the basic principles of how it works and how to use it correctly remain the same.
