# Avalanche Warp Messaging

> **Warning**
> Avalanche Warp Messaging is currently in experimental mode to be used only on ephemeral test networks.
>
> Breaking changes to Avalanche Warp Messaging integration into Subnet-EVM may still be made.

Avalanche Warp Messaging offers a basic primitive to enable Cross-Subnet communication on the Avalanche Network.

It is intended to allow communication between arbitrary Custom Virtual Machines (including, but not limited to Subnet-EVM).

## How does Avalanche Warp Messaging Work

Avalanche Warp Messaging relies on the Avalanche P-Chain to provide a read-only view of every Subnet's validator set. In Avalanche, the P-Chain is used to maintain the Primary Network's validator set, create new subnets and blockchains, and maintain the validator sets of each Subnet. As of the Banff Upgrade, Avalanche enables registering a BLS Public Key alongside a validator.

In order to be a validator of an Avalanche Subnet, a node must also validate the Avalanche Primary Network. This means each Subnet validator has read access to the P-Chain state.

With just those two things:
- Read access to all Subnet validator sets through the P-Chain
- BLS Public Keys registered on the P-Chain

We can build a generic Avalanche Warp Messaging Protocol. :point_down:

### Subnet to Subnet

The validator set of Subnet A can send a message on behalf of any blockchain on its Subnet.

For example, let's say that there are two Subnets: Subnet A and Subnet B each with a single Blockchain we'll call Blockchain A and Blockchain B.

First, Blockchain A produces a BLS Multisignature of a message to send to Blockchain B:

1. A transaction is issued on Blockchain A to send a message to Subnet B
2. The transaction is accepted on Blockchain A (must wait for acceptance)
3. The VM powering Blockchain A (ex: Subnet-EVM) should either
    a) be willing to sign the message to allow an off-chain relayer to aggregate signatures from the Subnet's validator set (existing implementation)
    b) aggregate signatures from the validator set of Subnet A to produce its own aggregate signature

The signature of Subnet A's validator set attests to the message being sent by Blockchain A.

To receive the message, Blockchain B must be running a Snowman VM that is wrapped in the Snowman++ ProposerVM. The ProposerVM provides the P-Chain Context which a block on Blockchain B was issued in. See [here](https://github.com/ava-labs/avalanchego/tree/v1.10.4/vms/proposervm#snowman-block-extension) for more details on the ProposerVM block wrapper. The ProposerVM header includes a P-Chain height at which the block was validated. This P-Chain height determines the canonical state of the P-Chain to use for verification of all Avalanche Warp Messages.

To validate and deliver the message, an off-chain component delivers the signed message from Blockchain A to Blockchain B (this is out of scope for Avalanche Warp Messaging itself).

When Blockchain B receives the message, it validates and delivers/executes the message:

1. Read the SourceChainID of the signed message (Blockchain A)
2. Look up the SubnetID that validates Blockchain A: Subnet A
3. Look up the validator set of Subnet A and the registered BLS Public Keys of Subnet A at the P-Chain height specified by the ProposerVM header
4. Filter the validators of Subnet A to include only the validators that the signed message claims as signers
5. Verify the claimed included validators represent a sufficient quorum of stake to verify the message
6. Aggregate the BLS Public Keys of the claimed signers into an aggregated BLS Public Key
7. Validate the aggregate signature matches the claimed aggregate BLS Public Key

After verifying the message, Blockchain B can define its own semantics of how to deliver or execute the message.

### Subnet to C-Chain (Primary Network)

Subnet to C-Chain Warp Messaging is a special case of Avalanche Warp Messaging. The C-Chain can efficiently verify a signature produced by another Subnet, so the implementation for the C-Chain to receive an Avalanche Warp Message can and will use the same code as is planned for Subnet-EVM.

### C-Chain to Subnet

To support C-Chain to Subnet communication, or more generally Primary Network to Subnet communication, we special case the C-Chain for two reasons:

1. Every Subnet validator validates the C-Chain
2. The Primary Network has the largest possible number of validators

Since the Primary Network has the largest possible number of validators for any Subnet on Avalanche, it would also be the most expensive Subnet to verify Avalanche Warp Messages from (most signatures required to verify it). Luckily, we can do something much smarter.

When a Subnet receives a message from a blockchain on the Primary Network, we use the validator set of the receiving Subnet instead of the entire network when validating the message. This means that the C-Chain sending a message can be the exact same as Subnet to Subnet communciation.

However, when Subnet B receives a message from the C-Chain, it changes the semantics to the following:

1. Read the SourceChainID of the signed message (C-Chain)
2. Look up the SubnetID that validates C-Chain: Primary Network
3. Look up the validator set of Subnet B (instead of the Primary Network) and the registered BLS Public Keys of Subnet B at the P-Chain height specified by the ProposerVM header
4. Filter the validators of Subnet B to include only the validators that the signed message claims as signers
5. Verify the claimed included validators represent a sufficient quorum of stake to verify the message
6. Aggregate the BLS Public Keys of the claimed signers into an aggregated BLS Public Key
7. Validate the aggregate signature matches the claimed aggregate BLS Public Key

This means that if Subnet B has 10 equally weighted validators, then C-Chain to Subnet communication only requires a threshold of stake from those 10 validators rather than a threshold of the stake of the Primary Network.

Since the security of Subnet B depends on the validators of Subnet B already, changing the requirements of verifying the message from verifying a signature from the entire Primary Network to only Subnet B's validator set does not change the security of Subnet B!

## Warp Precompile

The Warp Precompile is broken down into three functions defined in the Solidity interface file [here](../../../contracts/contracts/interfaces/IWarpMessenger.sol).

### sendWarpMessage

`sendWarpMessage` is used to send a verifiable message. Calling this function results in sending a message with the following contents:

- `SourceChainID` - blockchainID of the sourceChain on the Avalanche P-Chain
- `SourceAddress` - `msg.sender` encoded as a 32 byte value that calls `sendWarpMessage`
- `DestinationChainID` - `bytes32` argument specifies the blockchainID on the Avalanche P-Chain that should receive the message
- `DestinationAddress` - 32 byte value that represents the destination address that should receive the message (on the EVM this is the 20 byte address left zero extended)
- `Payload` - `payload` argument specified in the call to `sendWarpMessage` emitted as the unindexed data of the resulting log

Calling this function will issue a `SendWarpMessage` event from the Warp Precompile. Since the EVM limits the number of topics to 4 including the EventID, this message includes only the topics that would be expected to help filter messages emitted from the Warp Precompile the most.

Specifically, the `payload` is not emitted as a topic because each topic must be encoded as a hash. It could include the warp `messageID` as a topic, but that would not add more information. Therefore, we opt to take advantage of each possible topic to maximize the possible filtering for emitted Warp Messages.

Additionally, the `SourceChainID` is excluded because anyone parsing the chain can be expected to already know the blockchainID. Therefore, the `SendWarpMessage` event includes the indexable attributes:

- `destinationChainID`
- `destinationAddress`
- `sender`

The actual `message` is the entire [Avalanche Warp Unsigned Message](https://github.com/ava-labs/avalanchego/blob/master/vms/platformvm/warp/unsigned_message.go#L14) including the Subnet-EVM [Addressed Payload](../../../warp/payload/payload.go).


### getVerifiedMessage

`getVerifiedMessage` is used to read the contents of the delivered Avalanche Warp Message into the expected format.

It returns the message if present and a boolean indicating if a message is present.

To use this function, the transaction must include the signed Avalanche Warp Message encoded in the [predicate](#predicate-encoding) of the transaction. Prior to executing a block, the VM iterates through transactions and pre-verifies all predicates. If a transaction's predicate is invalid, then it is considered invalid to include in the block and dropped.

This gives the following properties:

1. The EVM execution does not need to verify the Warp Message at runtime (no signature verification or external calls to the P-Chain)
2. The EVM can deterministically re-execute and re-verify blocks assuming the predicate was verified by the network (eg., in bootstrapping)

This pre-verification is performed using the ProposerVM Block header during [block verification](../../../plugin/evm/block.go#L220) and [block building](../../../miner/worker.go#L200).

Note: in order to support the notion of an `AnycastID` for the `DestinationChainID`, `getVerifiedMessage` and the predicate DO NOT require that the `DestinationChainID` matches the `blockchainID` currently running. Instead, callers of `getVerifiedMessage` should use `getBlockchainID()` to decide how they should interpret the message. In other words, does the `destinationChainID` match either the local `blockchainID` or the `AnycastID`.

### getBlockchainID

`getBlockchainID` returns the blockchainID of the blockchain that Subnet-EVM is running on.

This is different from the conventional Ethereum ChainID registered to https://chainlist.org/.

The `blockchainID` in Avalanche refers to the txID that created the blockchain on the Avalanche P-Chain ([docs](https://docs.avax.network/specs/platform-transaction-serialization#unsigned-create-chain-tx)).

### Predicate Encoding

Avalanche Warp Messages are encoded as a signed Avalanche [Warp Message](https://github.com/ava-labs/avalanchego/blob/v1.10.4/vms/platformvm/warp/message.go#L7) where the [UnsignedMessage](https://github.com/ava-labs/avalanchego/blob/v1.10.4/vms/platformvm/warp/unsigned_message.go#L14)'s payload includes an [AddressedPayload](../../../warp/payload/payload.go).

Since the predicate is encoded into the [Transaction Access List](https://eips.ethereum.org/EIPS/eip-2930), it is packed into 32 byte hashes intended to declare storage slots that should be pre-warmed into the cache prior to transaction execution.

Therefore, we use the [Predicate Utils](../../../utils/predicate/README.md) package to encode the actual byte slice of size N into the access list.

## Design Considerations

### Re-Processing Historical Blocks

Avalanche Warp Messaging depends on the Avalanche P-Chain state at the P-Chain height specified by the ProposerVM block header.

Verifying a message requires looking up the validator set of the source subnet on the P-Chain. To support this, Avalanche Warp Messaging uses the ProposerVM header, which includes the P-Chain height it was issued at as the canonical point to lookup the source subnet's validator set.

This means verifying the Warp Message and therefore the state transition on a block depends on state that is external to the blockchain itself: the P-Chain.

The Avalanche P-Chain tracks only its current state and reverse diff layers (reversing the changes from past blocks) in order to re-calculate the validator set at a historical height. This means calculating a very old validator set that is used to verify a Warp Message in an old block may become prohibitively expensive.

Therefore, we need a heuristic to ensure that the network can correctly re-process old blocks (note: re-processing old blocks is a requirement to perform bootstrapping and is used in some VMs including Subnet-EVM to serve or verify historical data).

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

The Warp Precompile was designed with the intention of minimizing the trusted computing base for Subnet-EVM. Therefore, it makes several tradeoffs which encourage users to use protocols built ON TOP of the Warp Precompile itself as opposed to directly using the Warp Precompile.

The Warp Precompile itself provides ONLY the following ability:

send a verified message from a caller on blockchain A to a destination address on blockchain B

#### Explicitly Not Provided / Built on Top

The Warp Precompile itself does not provide any guarantees of:

- Eventual message delivery (may require re-send on blockchain A and additional assumptions about off-chain relayers and chain progress)
- Ordering of messages (requires ordering provided a layer above)
- Replay protection (requires replay protection provided a layer above)
