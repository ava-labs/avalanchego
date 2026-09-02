//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "precompile/contracts/warp/warpbindings/IWarpMessenger.sol";

// WarpTest invokes the warp precompile through IWarpMessenger to verify
// that the Solidity interface matches the precompile implementation.
contract WarpTest {
  IWarpMessenger private warp;

  constructor(address warpPrecompile) {
    warp = IWarpMessenger(warpPrecompile);
  }

  // Calls the getBlockchainID function on the precompile
  function getBlockchainID() external view returns (bytes32) {
    return warp.getBlockchainID();
  }

  // Calls the sendWarpMessage function on the precompile
  function sendWarpMessage(bytes calldata payload) external returns (bytes32 messageID) {
    return warp.sendWarpMessage(payload);
  }

  // Calls the getVerifiedWarpMessage function on the precompile
  function getVerifiedWarpMessage(
    uint32 index
  ) external view returns (WarpMessage memory message, bool valid) {
    return warp.getVerifiedWarpMessage(index);
  }

  // Calls the getVerifiedWarpBlockHash function on the precompile
  function getVerifiedWarpBlockHash(
    uint32 index
  ) external view returns (WarpBlockHash memory warpBlockHash, bool valid) {
    return warp.getVerifiedWarpBlockHash(index);
  }
}
