# Avalanche Warp Messaging

Avalanche Warp Messaging (AWM) provides a primitive for cross-subnet communication on the Avalanche Network.

The Avalanche P-Chain provides an index of every Subnet's validator set on the Avalanche Network, including the BLS public key of each validator (as of the [Banff Upgrade](https://github.com/ava-labs/avalanchego/releases/v1.9.0)). AWM utilizes the weighted validator sets stored on the P-Chain to build a cross-subnet communication protocol between any two Subnets on the Avalanche Network.

Any Virtual Machine (VM) on Avalanche can integrate Avalanche Warp Messaging to send and receive messages across Avalanche Subnets.

## Background

This README assumes familiarity with:

- Avalanche P-Chain / [PlatformVM](../)
- [ProposerVM](../../proposervm/README.md)
- Basic familiarity with [BLS Multi-Signatures](https://crypto.stanford.edu/~dabo/pubs/papers/BLSmultisig.html)

## BLS Multi-Signatures with Public-Key Aggregation

Avalanche Warp Messaging utilizes BLS multi-signatures with public key aggregation in order to verify messages signed by another Subnet. When a validator joins a Subnet, the P-Chain records the validator's BLS public key and NodeID, as well as a proof of possession of the validator's BLS private key to defend against [rogue public-key attacks](https://crypto.stanford.edu/~dabo/pubs/papers/BLSmultisig.html#mjx-eqn-eqaggsame).

AWM utilizes the validator set's weights and public keys to verify that an aggregate signature has sufficient weight signing the message from the source Subnet.

BLS provides a way to aggregate signatures off chain into a single signature that can be efficiently verified on chain.

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
                             |  40 + size(payload) bytes|
                             +--------------------------+
```

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

BitSetSignatures are verified within the context of a specific P-Chain height. At any given P-Chain height, the PlatformVM serves a canonically ordered validator set for the source subnet (validator set is ordered lexicographically by the BLS public key's byte representation). The `signers` bitset encodes which validator signatures were included. A value of `1` at index `i` in `signers` bitset indicates that a corresponding signature from the same validator at index `i` in the canonical validator set was included in the aggregate signature.

The bitset tells the verifier which BLS public keys should be aggregated to verify the warp message.

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

A blockchain on Avalanche sends an Avalanche Warp Message by coming to agreement on the message that every validator should be willing to sign. As an example, the VM of a blockchain may define that once a block is accepted, the VM should be willing to sign a message including the block hash in the payload to attest to any other Subnet that the block was accepted. The contents of the payload, how to aggregate the signature (VM-to-VM communication, off-chain relayer, etc.), is left to the VM.

Once the validator set of a blockchain is willing to sign an arbitrary message `M`, an aggregator performs the following process:

1. Gather signatures of the message `M` from `N` validators (where the `N` validators meet the required threshold of stake on the destination chain)
2. Aggregate the `N` signatures into a multi-signature
3. Look up the canonical validator set at the P-Chain height where the message will be verified
4. Encode the selection of the `N` validators included in the signature in a bitset
5. Construct the signed message from the aggregate signature, bitset, and original unsigned message

## Verifying / Receiving an Avalanche Warp Message

Avalanche Warp Messages are verified within the context of a specific P-Chain height included in the [ProposerVM](../../proposervm/README.md)'s header. The P-Chain height is provided as context to the underlying VM when verifying the underlying VM's blocks (implemented by the optional interface [WithVerifyContext](../../../snow/engine/snowman/block/block_context_vm.go)).

To verify the message, the underlying VM utilizes this `warp` package to perform the following steps:

1. Lookup the canonical validator set of the Subnet sending the message at the P-Chain height
2. Filter the canonical validator set to only the validators claimed by the signature
3. Verify the weight of the included validators meets the required threshold defined by the receiving VM
4. Aggregate the public keys of the claimed validators into a single aggregate public key
5. Verify the aggregate signature of the unsigned message against the aggregate public key

Once a message is verified, it is left to the VM to define the semantics of delivering a verified message.

## Design Considerations

### Processing Historical Avalanche Warp Messages

Verifying an Avalanche Warp Message requires a lookup of validator sets at a specific P-Chain height. The P-Chain serves lookups maintaining validator set diffs that can be applied in-order to reconstruct the validator set of any Subnet at any height.

As the P-Chain grows, the number of validator set diffs that needs to be applied in order to reconstruct the validator set needed to verify an Avalanche Warp Messages increases over time.

Therefore, in order to support verifying historical Avalanche Warp Messages, VMs should provide a mechanism to determine whether an Avalanche Warp Message was treated as valid or invalid within a historical block.

When nodes bootstrap in the future, they bootstrap blocks that have already been marked as accepted by the network, so they can assume the block was verified by the validators of the network when it was first accepted.

Therefore, the new bootstrapping node can assume the block was valid to determine whether an Avalanche Warp Message should be treated as valid/invalid within the execution of that block.

Two strategies to provide that mechanism are:

- Require warp message validity for transaction inclusion. If the transaction is included, the warp message must have passed verification.
- Include the results of warp message verification in the block itself. Use the results to determine which messages passed verification.

