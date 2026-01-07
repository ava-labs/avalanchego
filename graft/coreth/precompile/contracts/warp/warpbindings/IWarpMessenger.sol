// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// SPDX-License-Identifier: MIT

pragma solidity ^0.8.0;

struct WarpMessage {
  bytes32 sourceChainID;
  address originSenderAddress;
  bytes payload;
}

struct WarpBlockHash {
  bytes32 sourceChainID;
  bytes32 blockHash;
}

interface IWarpMessenger {
  event SendWarpMessage(address indexed sender, bytes32 indexed messageID, bytes message);

  // sendWarpMessage emits a request for the subnet to send a warp message from [msg.sender]
  // with the specified parameters.
  // This emits a SendWarpMessage log from the precompile. When the corresponding block is accepted
  // the Accept hook of the Warp precompile is invoked with all accepted logs emitted by the Warp
  // precompile.
  // Each validator then adds the UnsignedWarpMessage encoded in the log to the set of messages
  // it is willing to sign for an off-chain relayer to aggregate Warp signatures.
  function sendWarpMessage(bytes calldata payload) external returns (bytes32 messageID);

  // getVerifiedWarpMessage parses the pre-verified warp message in the
  // predicate storage slots as a WarpMessage and returns it to the caller.
  // If the message exists and passes verification, returns the verified message
  // and true.
  // Otherwise, returns false and the empty value for the message.
  function getVerifiedWarpMessage(uint32 index) external view returns (WarpMessage calldata message, bool valid);

  // getVerifiedWarpBlockHash parses the pre-verified WarpBlockHash message in the
  // predicate storage slots as a WarpBlockHash message and returns it to the caller.
  // If the message exists and passes verification, returns the verified message
  // and true.
  // Otherwise, returns false and the empty value for the message.
  function getVerifiedWarpBlockHash(
    uint32 index
  ) external view returns (WarpBlockHash calldata warpBlockHash, bool valid);

  // getBlockchainID returns the snow.Context BlockchainID of this chain.
  // This blockchainID is the hash of the transaction that created this blockchain on the P-Chain
  // and is not related to the Ethereum ChainID.
  function getBlockchainID() external view returns (bytes32 blockchainID);
}
