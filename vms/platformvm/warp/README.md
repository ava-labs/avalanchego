# Avalanche Interchain Messaging

Avalanche Interchain Messaging (ICM) provides a primitive for cross-chain communication on the Avalanche Network.

The Avalanche P-Chain provides an index of every network's validator set on the Avalanche Network, including the BLS public key of each validator (as of the [Banff Upgrade](https://github.com/ava-labs/avalanchego/releases/v1.9.0)). ICM utilizes the weighted validator sets stored on the P-Chain to build a cross-chain communication protocol between any two networks on Avalanche.

Any Virtual Machine (VM) on Avalanche can integrate Avalanche Interchain Messaging to send and receive messages cross-chain.

## Background

This README assumes familiarity with:

- Avalanche P-Chain / [PlatformVM](../)
- [ProposerVM](../../proposervm/README.md)
- Basic familiarity with [BLS Multi-Signatures](https://crypto.stanford.edu/~dabo/pubs/papers/BLSmultisig.html)

## BLS Multi-Signatures with Public-Key Aggregation

Avalanche Interchain Messaging utilizes BLS multi-signatures with public key aggregation in order to verify messages signed by another network. When a validator joins a network, the P-Chain records the validator's BLS public key and NodeID, as well as a proof of possession of the validator's BLS private key to defend against [rogue public-key attacks](https://crypto.stanford.edu/~dabo/pubs/papers/BLSmultisig.html#mjx-eqn-eqaggsame).

ICM utilizes the validator set's weights and public keys to verify that an aggregate signature has sufficient weight signing the message from the source network.

BLS provides a way to aggregate signatures off chain into a single signature that can be efficiently verified on chain.

## ICM Serialization

Unsigned Message:

```
+---------------+----------+--------------------------+
|      codecID  :  uint16  |                 2 bytes  |
+---------------+----------+--------------------------+
|     networkID :  uint32  |                 4 bytes  |
+---------------+----------+--------------------------+
| sourceChainId : [32]byte |                32 bytes  |
+---------------+----------+--------------------------+
|       payload :   []byte |       4 + size(payload)  |
+---------------+----------+--------------------------+
                           |  42 + size(payload) bytes|
                           +--------------------------+
```

- `codecID` is the codec version used to serialize the payload and is hardcoded to `0x0000`
- `networkID` is the unique ID of an Avalanche Network (Mainnet/Testnet) and provides replay protection for BLS Signers across different Avalanche Networks
- `sourceChainID` is the hash of the transaction that created the blockchain on the Avalanche P-Chain. It serves as the unique identifier for the blockchain across the Avalanche Network so that each blockchain can only sign a message with its own id.
- `payload` provides an arbitrary byte array containing the contents of the message. VMs define their own message types to include in the `payload`

BitSetSignature:

```
+-----------+----------+---------------------------+
|   type_id :   uint32 |                   4 bytes |
+-----------+----------+---------------------------+
|   signers :   []byte |          4 + len(signers) |
+-----------+----------+---------------------------+
| signature : [96]byte |                  96 bytes |
+-----------+----------+---------------------------+
                       | 104 + size(signers) bytes |
                       +---------------------------+
```

- `typeID` is the ID of this signature type, which is `0x00000000`
- `signers` encodes a bitset of which validators' signatures are included (a bitset is a byte array where each bit indicates membership of the element at that index in the set)
- `signature` is an aggregated BLS Multi-Signature of the Unsigned Message

BitSetSignatures are verified within the context of a specific P-Chain height. At any given P-Chain height, the PlatformVM serves a canonically ordered validator set for the source network (validator set is ordered lexicographically by the BLS public key's byte representation). The `signers` bitset encodes which validator signatures were included. A value of `1` at index `i` in `signers` bitset indicates that a corresponding signature from the same validator at index `i` in the canonical validator set was included in the aggregate signature.

The bitset tells the verifier which BLS public keys should be aggregated to verify the interchain message.

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

## Sending an Avalanche Interchain Message

A blockchain on Avalanche sends an Avalanche Interchain Message by coming to agreement on the message that every validator should be willing to sign. As an example, the VM of a blockchain may define that once a block is accepted, the VM should be willing to sign a message including the block hash in the payload to attest to any other network that the block was accepted. The contents of the payload, how to aggregate the signature (VM-to-VM communication, off-chain relayer, etc.), is left to the VM.

Once the validator set of a blockchain is willing to sign an arbitrary message `M`, an aggregator performs the following process:

1. Gather signatures of the message `M` from `N` validators (where the `N` validators meet the required threshold of stake on the destination chain)
2. Aggregate the `N` signatures into a multi-signature
3. Look up the canonical validator set at the P-Chain height where the message will be verified
4. Encode the selection of the `N` validators included in the signature in a bitset
5. Construct the signed message from the aggregate signature, bitset, and original unsigned message

## Verifying / Receiving an Avalanche Interchain Message

Avalanche Interchain Messages are verified within the context of a specific P-Chain height included in the [ProposerVM](../../proposervm/README.md)'s header. The P-Chain height is provided as context to the underlying VM when verifying the underlying VM's blocks (implemented by the optional interface [WithVerifyContext](../../../snow/engine/snowman/block/block_context_vm.go)).

To verify the message, the underlying VM utilizes this `warp` package to perform the following steps:

1. Lookup the canonical validator set of the network sending the message at the P-Chain height
2. Filter the canonical validator set to only the validators claimed by the signature
3. Verify the weight of the included validators meets the required threshold defined by the receiving VM
4. Aggregate the public keys of the claimed validators into a single aggregate public key
5. Verify the aggregate signature of the unsigned message against the aggregate public key

Once a message is verified, it is left to the VM to define the semantics of delivering a verified message.

## Design Considerations

### Processing Historical Avalanche Interchain Messages

Verifying an Avalanche Interchain Message requires a lookup of validator sets at a specific P-Chain height. The P-Chain serves lookups maintaining validator set diffs that can be applied in-order to reconstruct the validator set of any network at any height.

As the P-Chain grows, the number of validator set diffs that needs to be applied in order to reconstruct the validator set needed to verify an Avalanche Interchain Messages increases over time.

Therefore, in order to support verifying historical Avalanche Interchain Messages, VMs should provide a mechanism to determine whether an Avalanche Interchain Message was treated as valid or invalid within a historical block.

When nodes bootstrap in the future, they bootstrap blocks that have already been marked as accepted by the network, so they can assume the block was verified by the validators of the network when it was first accepted.

Therefore, the new bootstrapping node can assume the block was valid to determine whether an Avalanche Interchain Message should be treated as valid/invalid within the execution of that block.

Two strategies to provide that mechanism are:

- Require interchain message validity for transaction inclusion. If the transaction is included, the interchain message must have passed verification.
- Include the results of interchain message verification in the block itself. Use the results to determine which messages passed verification.
