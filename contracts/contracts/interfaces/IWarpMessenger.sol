// (c) 2022-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// SPDX-License-Identifier: MIT

pragma solidity ^0.8.0;

struct WarpMessage {
    bytes32 originChainID;
    address originSenderAddress;
    bytes32 destinationChainID;
    address destinationAddress;
    bytes payload;
}

interface WarpMessenger {
    event SendWarpMessage(
        bytes32 indexed destinationChainID,
        address indexed destinationAddress,
        address indexed sender,
        bytes message
    );

    // sendWarpMessage emits a request for the subnet to send a warp message from [msg.sender]
    // with the specified parameters.
    // This emits a SendWarpMessage log from the precompile. When the corresponding block is accepted
    // the Accept hook of the Warp precompile is invoked with all accepted logs emitted by the Warp
    // precompile.
    // Each validator then adds the UnsignedWarpMessage encoded in the log to the set of messages
    // it is willing to sign for an off-chain relayer to aggregate Warp signatures.
    function sendWarpMessage(
        bytes32 destinationChainID,
        address destinationAddress,
        bytes calldata payload
    ) external;

    // getVerifiedWarpMessage parses the pre-verified warp message in the
    // predicate storage slots as a WarpMessage and returns it to the caller.
    // Returns false if no such predicate exists.
    function getVerifiedWarpMessage()
        external view
        returns (WarpMessage calldata message, bool exists);

    // Note: getVerifiedWarpMessage takes no arguments because it returns a single verified
    // message that is encoded in the predicate (inside the tx access list) of the transaction.
    // The alternative design to this is to verify messages during the EVM's execution in which
    // case there would be no predicate and the block would encode the hits/misses that occur
    // throughout its execution.
    // This would result in the following alternative function signature:
    // function verifyMessage(bytes calldata signedWarpMsg) external returns (WarpMessage calldata message);

    // getBlockchainID returns the snow.Context BlockchainID of this chain.
    // This blockchainID is the hash of the transaction that created this blockchain on the P-Chain
    // and is not related to the Ethereum ChainID.
    function getBlockchainID() external view returns (bytes32 blockchainID);
}
