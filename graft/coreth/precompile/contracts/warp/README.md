# Integrating Avalanche Warp Messaging into the EVM

Avalanche Warp Messaging offers a basic primitive to enable Cross-L1 communication on the Avalanche Network.

It is intended to allow communication between arbitrary Custom Virtual Machines (including, but not limited to Subnet-EVM and Coreth).

## How does Avalanche Warp Messaging Work?

Avalanche Warp Messaging uses BLS Multi-Signatures with Public-Key Aggregation where every Avalanche validator registers a public key alongside its NodeID on the Avalanche P-Chain.

Every node tracking an Avalanche L1 has read access to the Avalanche P-Chain. This provides weighted sets of BLS Public Keys that correspond to the validator sets of each L1 on the Avalanche Network. Avalanche Warp Messaging provides a basic primitive for signing and verifying messages between L1s: the receiving network can verify whether an aggregation of signatures from a set of source L1 validators represents a threshold of stake large enough for the receiving network to process the message.

For more details on Avalanche Warp Messaging, see the AvalancheGo [Warp README](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/warp/README.md).

### Flow of Sending / Receiving a Warp Message within the EVM

The Avalanche Warp Precompile enables this flow to send a message from blockchain A to blockchain B:

1. Call the Warp Precompile `sendWarpMessage` function with the arguments for the `UnsignedMessage`
2. Warp Precompile emits an event / log containing the `UnsignedMessage` specified by the caller of `sendWarpMessage`
3. Network accepts the block containing the `UnsignedMessage` in the log, so that validators are willing to sign the message
4. An off-chain relayer queries the validators for their signatures of the message and aggregates the signatures to create a `SignedMessage`
5. The off-chain relayer encodes the `SignedMessage` as the [predicate](#predicate-encoding) in the AccessList of a transaction to deliver on blockchain B
6. The transaction is delivered on blockchain B, the signature is verified prior to executing the block, and the message is accessible via the Warp Precompile's `getVerifiedWarpMessage` during the execution of that transaction

### Warp Precompile

The Warp Precompile is broken down into three functions defined in the Solidity interface file [here](../../../contracts/contracts/interfaces/IWarpMessenger.sol).

#### sendWarpMessage

`sendWarpMessage` is used to send a verifiable message. Calling this function results in sending a message with the following contents:

- `SourceChainID` - blockchainID of the sourceChain on the Avalanche P-Chain
- `SourceAddress` - `msg.sender` encoded as a 32 byte value that calls `sendWarpMessage`
- `Payload` - `payload` argument specified in the call to `sendWarpMessage` emitted as the unindexed data of the resulting log

Calling this function will issue a `SendWarpMessage` event from the Warp Precompile. Since the EVM limits the number of topics to 4 including the EventID, this message includes only the topics that would be expected to help filter messages emitted from the Warp Precompile the most.

Specifically, the `payload` is not emitted as a topic because each topic must be encoded as a hash. Therefore, we opt to take advantage of each possible topic to maximize the possible filtering for emitted Warp Messages.

Additionally, the `SourceChainID` is excluded because anyone parsing the chain can be expected to already know the blockchainID. Therefore, the `SendWarpMessage` event includes the indexable attributes:

- `sender`
- The `messageID` of the unsigned message (sha256 of the unsigned message)

The actual `message` is the entire [Avalanche Warp Unsigned Message](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/warp/unsigned_message.go#L14) including an [AddressedCall](https://github.com/ava-labs/avalanchego/tree/master/vms/platformvm/warp/payload#addressedcall). The unsigned message is emitted as the unindexed data in the log.

#### getVerifiedMessage

`getVerifiedMessage` is used to read the contents of the delivered Avalanche Warp Message into the expected format.

It returns the message if present and a boolean indicating if a message is present.

To use this function, the transaction must include the signed Avalanche Warp Message encoded in the [predicate](#predicate-encoding) of the transaction. Prior to executing a block, the VM iterates through transactions and pre-verifies all predicates. If a transaction's predicate is invalid, then it is considered invalid to include in the block and dropped.

This leads to the following advantages:

1. The EVM execution does not need to verify the Warp Message at runtime (no signature verification or external calls to the P-Chain)
2. The EVM can deterministically re-execute and re-verify blocks assuming the predicate was verified by the network (e.g., in bootstrapping)

This pre-verification is performed using the ProposerVM Block header during [block verification](../../../plugin/evm/block.go#L355) & [block building](../../../miner/worker.go#L200).

#### getBlockchainID

`getBlockchainID` returns the blockchainID of the blockchain that the VM is running on.

This is different from the conventional Ethereum ChainID registered to [ChainList](https://chainlist.org/).

The `blockchainID` in Avalanche refers to the txID that created the blockchain on the Avalanche P-Chain ([docs](https://docs.avax.network/specs/platform-transaction-serialization#unsigned-create-chain-tx)).

### Predicate Encoding

Avalanche Warp Messages are encoded as a signed Avalanche [Warp Message](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/warp/message.go) where the [UnsignedMessage](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/warp/unsigned_message.go)'s payload includes an [AddressedPayload](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/warp/payload/payload.go).

Since the predicate is encoded into the [Transaction Access List](https://eips.ethereum.org/EIPS/eip-2930), it is packed into 32 byte hashes intended to declare storage slots that should be pre-warmed into the cache prior to transaction execution.

Therefore, we use the [Predicate Utils](https://github.com/ava-labs/avalanchego/graft/coreth/blob/master/predicate/Predicate.md) package to encode the actual byte slice of size N into the access list.

### Performance Optimization: C-Chain to Avalanche L1

For communication between the C-Chain and an L1, as well as broader interactions between the Primary Network and Avalanche L1s, we have implemented special handling for the C-Chain.

The Primary Network has a large validator set, which creates a unique challenge for Avalanche Warp messages. To reach the required stake threshold, numerous signatures would need to be collected and verifying messages from the Primary Network would be computationally costly. However, we have developed a more efficient solution.

When an Avalanche L1 receives a message from a blockchain on the Primary Network, we use the validator set of the receiving L1 instead of the entire network when validating the message. Note this is NOT possible if an L1 does not validate the Primary Network, in which case the Warp precompile must be configured with `requirePrimaryNetworkSigners`.

Sending messages from the C-Chain remains unchanged.
However, when L1 XYZ receives a message from the C-Chain, it changes the semantics to the following:

1. Read the `SourceChainID` of the signed message (C-Chain)
2. Look up the `SubnetID` that validates C-Chain: Primary Network
3. Look up the validator set of L1 XYZ (instead of the Primary Network) and the registered BLS Public Keys of L1 XYZ at the P-Chain height specified by the ProposerVM header
4. Continue Warp Message verification using the validator set of L1 XYZ instead of the Primary Network

This means that C-Chain to L1 communication only requires a threshold of stake on the receiving L1 to sign the message instead of a threshold of stake for the entire Primary Network.

This assumes that the security of L1 XYZ already depends on the validators of L1 XYZ to behave virtuously. Therefore, requiring a threshold of stake from the receiving L1's validator set instead of the whole Primary Network does not meaningfully change security of the receiving L1.

Note: this special case is ONLY applied during Warp Message verification. The message sent by the Primary Network will still contain the Avalanche C-Chain's blockchainID as the sourceChainID and signatures will be served by querying the C-Chain directly.

## Design Considerations

### Re-Processing Historical Blocks

Avalanche Warp Messaging depends on the Avalanche P-Chain state at the P-Chain height specified by the ProposerVM block header.

Verifying a message requires looking up the validator set of the source L1 on the P-Chain. To support this, Avalanche Warp Messaging uses the ProposerVM header, which includes the P-Chain height it was issued at as the canonical point to lookup the source L1's validator set.

This means verifying the Warp Message and therefore the state transition on a block depends on state that is external to the blockchain itself: the P-Chain.

The Avalanche P-Chain tracks only its current state and reverse diff layers (reversing the changes from past blocks) in order to re-calculate the validator set at a historical height. This means calculating a very old validator set that is used to verify a Warp Message in an old block may become prohibitively expensive.

Therefore, we need a heuristic to ensure that the network can correctly re-process old blocks (note: re-processing old blocks is a requirement to perform bootstrapping and is used in some VMs to serve or verify historical data).

As a result, we require that the block itself provides a deterministic hint which determines which Avalanche Warp Messages were considered valid/invalid during the block's execution. This ensures that we can always re-process blocks and use the hint to decide whether an Avalanche Warp Message should be treated as valid/invalid even after the P-Chain state that was used at the original execution time may no longer support fast lookups.

To provide that hint, we've explored two designs:

1. Include a predicate in the transaction to ensure any referenced message is valid
2. Append the results of checking whether a Warp Message is valid/invalid to the block data itself

The current implementation uses option (1).

The original reason for this was that the notion of predicates for precompiles was designed with Shared Memory in mind. In the case of shared memory, there is no canonical "P-Chain height" in the block which determines whether or not Avalanche Warp Messages are valid.

Instead, the VM interprets a shared memory import operation as valid as soon as the UTXO is available in shared memory. This means that if it were up to the block producer to staple the valid/invalid results of whether or not an attempted atomic operation should be treated as valid, a byzantine block producer could arbitrarily report that such atomic operations were invalid and cause a griefing attack to burn the gas of users that attempted to perform an import.

Therefore, a transaction specified predicate is required to implement the shared memory precompile to prevent such a griefing attack.

In contrast, Avalanche Warp Messages are validated within the context of an exact P-Chain height. Therefore, if a block producer attempted to lie about the validity of such a message, the network would interpret that block as invalid.

### Guarantees Offered by Warp Precompile vs. Built on Top

#### Guarantees Offered by Warp Precompile

The Warp Precompile was designed with the intention of minimizing the trusted computing base for the VM as much as possible. Therefore, it makes several tradeoffs which encourage users to use protocols built ON TOP of the Warp Precompile itself as opposed to directly using the Warp Precompile.

The Warp Precompile itself provides ONLY the following ability:

- Emit a verifiable message from (Address A, Blockchain A) to (Address B, Blockchain B) that can be verified by the destination chain

#### Explicitly Not Provided / Built on Top

The Warp Precompile itself does not provide any guarantees of:

- Eventual message delivery (may require re-send on blockchain A and additional assumptions about off-chain relayers and chain progress)
- Ordering of messages (requires ordering provided a layer above)
- Replay protection (requires replay protection provided a layer above)
