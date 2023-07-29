# Avalanche Warp Messaging

Avalanche Warp Messaging (AWM) provides a primitive for Cross-Subnet Communication on the Avalanche Network.

The Avalanche P-Chain provides an index of every Subnet's validator set on the Avalanche Network including a BLS Public Key registered to validators (as of [Banff Upgrade](https://github.com/ava-labs/avalanchego/releases/v1.9.0)). AWM utilizes the weighted validator sets stored on the P-Chain and the registered BLS Public Keys to build a Cross-Subnet Communication Protocol between any two Subnets on the Avalanche Network.

Any Virtual Machine (VM) on Avalanche, can integrate Avalanche Warp Messaging to send and receive verifiable messages across Subnets.

## BLS Multi-Signatures with Public-Key Aggregation

Avalanche Warp Messaging utilizes BLS Multi-Signatures with Public-Key Aggregation in order to verify messages signed by another Subnet. To accomplish this, when a validator is added to the Avalanche P-Chain, it registers a BLS Public-Key to its NodeID along with a Proof of Possession of the corresponding private key to defend against a rogue public-key attack.

The P-Chain already provides the weighted validator set for every Subnet used in consensus. AWM relies on the registered BLS Public Keys to each validator to provide a weighted set of BLS Public Keys to verify a messaage coming from any Subnet.

BLS Multi-Signatures with Public-Key Aggregation provides a way to aggregate BLS private keys, public keys, and signatures into a single key/signature.

For more information on the cryptographic primitive underlying Warp, see [here](https://crypto.stanford.edu/~dabo/pubs/papers/BLSmultisig.html).

## AWM Serialization

Unsigned Message:
```
+-----------------+----------+--------------------------+
|      network_id :  uint32  |                 4 bytes  |
+-----------------+----------+--------------------------+
| source_chain_id : [32]byte |                32 bytes  |
+-----------------+----------+--------------------------+
|         payload :   []byte |       4 + size(payload)  |
+-----------------+----------+--------------------------+
                             |  38 + size(payload) bytes|
                             +--------------------------+
```

- `networkID` provides replay protection for BLS Signers across different Avalanche Networks
- `sourceChainID` ensures that each blockchain on Avalanche can only sign a message with its own `blockchainID`
- `payload` is a byte array containing the contents of the message. VMs can define their own message types to include in the `payload`

Note: the `blockchainID` is the hash of the transaction that created the blockchain on the Avalanche P-Chain. It serves as the unique identifier for the blockchain across the Avalanche Network.

BitSet Signature:
```
+-----------+----------+---------------------------+
|   signers :   []byte |          4 + len(signers) |
+-----------+----------+---------------------------+
| signature : [96]byte |                  96 bytes |
+-----------+----------+---------------------------+
                       | 100 + size(signers) bytes |
                       +---------------------------+
```

- `signers` encodes a BitSet of which validators' signatures are included
- `signature` is an aggregated BLS Multi-Signature of the Unsigned Message

BitSet Signatures are verified within the context of a specific P-Chain height. At a specific P-Chain height, the PlatformVM can serve a canonically ordered validator set for the source subnet. The signers BitSet encodes which validators' signature was included. A 1 at index i in `signers` claims that a signature from the validator at index i in the canonical validator set is included in the aggregate signature.

The BitSet tells the verifier which BLS Public Keys should be aggregated to verify the Warp Message.

Signed Message:
```
+------------------+------------------+-------------------------------------------------+
| unsigned_message :  UnsignedMessage |                          size(unsigned_message) |
+------------------+------------------+-------------------------------------------------+
|        signature :        Signature |                                 size(signature) |
+------------------+------------------+-------------------------------------------------+
                                      |  size(unsigned_message) + size(signature) bytes |
                                      +-------------------------------------------------+
```

## Sending an Avalanche Warp Message

A Blockchain on Avalanche sends an Avalanche Warp Message by coming to agreement on the message that every validator should be willing to sign. As an example, the VM of a Blockchain may define that once a block is accepted, the VM should be willing to sign a message including the block hash in the payload to attest to any other Subnet that the block was accepted. The contents of the payload, how to aggregate the signature (VM-to-VM communication, off-chain relayer, etc.), is left to the VM.

Once the validator set of a blockchain is willing to sign the message M, an aggregator can perform the following process:

1. Gather signatures of the message M from N validators (assume N validators meets the required threshold of stake)
2. Aggregate the N signatures into a multi-signature
3. Look up the canonical validator set at the P-Chain height where the message will be verified
4. Encode the selection of N validators included in the signature out of the canonical validator set as a BitSet
5. Construct the Signed Message with the aggregate signature, BitSet, and the Unsigned Message

## Verifying / Receiving an Avalanche Warp Message

Avalanache Warp Messages are verified within the context of a specific P-Chain height that is included in the [ProposerVM](../../proposervm/README.md)'s header. The P-Chain height is provided as context to the wrapped VM when verifying the VM's internal blocks (implemented by the optional interface [WithVerifyContext](../../../snow/engine/snowman/block/block_context_vm.go)).

To verify the message, the VM utilizes this Warp package to perform the following steps:

1. Lookup the canonical validator set of the Subnet sending the message at the P-Chain height
2. Filter the canonical validator set to only the validators claimed by the BitSetSignature
3. Verify the weight of the included validators meets the required threshold
4. Aggregate the public keys of the claimed validators into a single aggregate public key
5. Verify the aggregate signature of the unsigned message against the aggregate public key

Once a message is verified, it is left to the VM to define the semantics of delivering a verified message.

## Design Considerations

### Processing Historical Avalanche Warp Messages

Verifying an Avalanche Warp Message requires a lookup of validator sets at a specific P-Chain height. The P-Chain serves lookups by maintaining reverse diffs that can be iterated in order to re-create the validator set of any Subnet at a specific height.

As a chain moves forward, the number of reverse diffs that need to be computed in order to look up the validator set needed to verify an Avalanche Warp Messages increases over time.

Therefore, in order to support verifying historical Avalanche Warp Messages, VMs should provide a heuristic to determine whether an Avalanche Warp Message was treated as valid or invalid within a historical block.

When nodes bootstrap in the future, they bootstrap blocks that have already been marked as accepted by the network, so they can assume the block was verified by the validators of the network when it was first accepted.

Therefore, the new bootstrapping node can use a heuristic encoded in the block to determine whether an Avalanche Warp Message should be treated as valid / invalid within the execution of that block.

Two strategies to provide that heuristic are:

- Encode the Warp Message as a predicate for transaction inclusion. If the transaction is included, the Warp Message must have passed verification.
- Staple the results of every Warp Message verification operation to the block itself. Use the stapled results to determine which messages passed verification.

