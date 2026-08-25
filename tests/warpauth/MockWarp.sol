// SPDX-License-Identifier: BSD-3-Clause
pragma solidity ^0.8.28;

/// Test double for the warp precompile: remembers the last payload.
contract MockWarp {
    bytes public last;

    function sendWarpMessage(bytes calldata payload) external returns (bytes32) {
        last = payload;
        return keccak256(payload);
    }
}
