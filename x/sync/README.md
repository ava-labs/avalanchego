# `sync` package

## Overview

This package implements a client and server that allows for the syncing of a [MerkleDB](../merkledb/README.md). The servers have an up to date version of the database, and the clients have an out of date version of the database or an empty database.

It's planned that these client and server implementations will eventually be compatible with Firewood.

## Messages

There are four message types sent between the client and server:

1. `SyncGetRangeProofRequest`
2. `RangeProof`
3. `SyncGetChangeProofRequest`
4. `SyncGetChangeProofResponse`

These message types are defined in `avalanchego/proto/sync.proto`.
For more information on range proofs and change proofs, see their definitions in `avalanchego/merkledb/proof.go`.

### `SyncGetRangeProofRequest`

This message is sent from the client to the server to request a range proof for a given key range and root ID. That is, the client says, "Give me the key-value pairs that were in this key range when the database had this root."

### `RangeProof`

This message is sent from the server to the client in response to a `SyncGetRangeProofRequest`. It contains the key-value pairs that were in the requested key range when the database had the requested root, as well as a proof that the key-value pairs are correct.

### `SyncGetChangeProofRequest`

This message is sent from the client to the server to request a change proof between the given root IDs. That is, the client says, "Give me the key-value pairs that changed between the time the database had this root and that root."

### `SyncGetChangeProofResponse`

This message is sent from the server to the client in response to a `SyncGetChangeProofRequest`. If the server had sufficient history to generate a change proof, it contains a change proof that contains the key-value pairs that changed between the requested roots. If the server did not have sufficient history to generate a change proof, it contains a range proof that contains the key-value pairs that were in the database when the database had the latter root.

## Algorithm

When the client starts with an empty database, it has no key-value pairs. It requests from a server a range proof for the entire database. The server replies with a range proof, which the client verifies. If it's valid, the key-value pairs in the proof are written to the database. If it's not, the client drops the proof and requests the proof from another server. 

A range proof sent by a server may be valid but not contain all of the key-value pairs in the requested range. For example, a client might request all the key-value pairs in [`start`, `end`] but only receive those in range [`start`, `end'`] where `end'` < `end`. There might be too many key-value pairs to include in one message, or the server may be too busy to provide any more in its response. Unless the database is very small, this means that the range proof the client receives in response to its range proof request for the entire database will not contain all of the key-value pairs in the database.

For each key range, the sync client keeps track of the root ID of the database revision for which it has downloaded that key range. For example, it will store information that says something like, "I have all of the key-value pairs that were in range [`start`, `end`] when the database's root was `rootID`" for some keys `start` and `end`, and some database `rootID`. Note that `rootID` is the root ID that the client is trying to sync to, not the root ID of its own incomplete database.

If the first range proof contained all of the key-value pairs up to some key `end`, the client recognizes that it must fetch all of the keys after `end`. It repeatedly requests range proofs or change proofs for chunks of the remaining key range until it has all of the key-value pairs in the database. Note that the client may split the remaining key range into chunks and fetch each chunk of key-value pairs in parallel.

However, the database may be changing as the client is syncing. The sync client can be notified that the root ID of the database it's trying to sync to has changed. Detecting that the root ID to sync to has changed is done outside this package. If this occurs, the key-value pairs the client has learned about via range proofs may no longer be valid.

We use change proofs to correct the out of date key-value pairs. When the sync client is notified that the root ID to sync to has changed, it requests a a change proof from a server for a given key range. For example, if a client has the key-value pairs in range [`start`, `end`] that were in the database when it had `rootID`, then it will request a change proof that provides all of the key-value changes in range [`start`, `end`] from the database version with root ID `rootID` to the database version with root ID `newRootID`. The client verifies the change proof, and if it's valid, it applies the changes to its database. If it's not, the client drops the proof and requests the proof from another server.

A server needs to have history in order to serve a change proof. Namely, it needs to know all of the database changes between two roots. If the server does not have sufficient history to generate a change proof, it will send a range proof instead. The client will verify and apply the range proof. Note that change proofs, like range proofs, may not contain all of the key-value pairs in the requested range. This is OK because as mentioned above, the client tracks the root ID associated with each range of key-value pairs it has, so it knows which key-value pairs are out of date. If a client requests the changes in range [`start`, `end`], but the server replies with all of the changes in [`start`, `end'`] for some `end'` < `end`, the client will request another change proof for the remaining key-value pairs (namely in [`end'`, `end`]). 

Note that for both range proofs and change proofs, if a server can't serve the entire requested key range in one response, its response will omit keys from the end of the range rather than the start. For example, if a client requests a range proof for range [`start`, `end`] but the server can't fit all the key-value pairs in one response, it'll send a range proof for [`start`, `end'`] where `end'` < `end`, as opposed to sending a range proof for [`start'`, `end`] where `start'` > `start`.

Eventually, the client will have all of the key-value pairs in the database. At this point, it's synced.

## TODOs

- [ ] Handle errors on proof requests.  Currently, any errors that occur server side are not sent back to the client.
- [ ] Handle missing roots in change proofs by returning a range proof instead of failing
