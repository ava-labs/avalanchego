//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;
pragma experimental ABIEncoderV2;

import "precompile/contracts/warp/warpbindings/IWarpMessenger.sol";

contract ExampleWarp {
    address constant WARP_ADDRESS = 0x0200000000000000000000000000000000000005;
    IWarpMessenger warp = IWarpMessenger(WARP_ADDRESS);

    // sendWarpMessage sends a warp message containing the payload
    function sendWarpMessage(bytes calldata payload) external {
        warp.sendWarpMessage(payload);
    }

    // validateWarpMessage retrieves the warp message attached to the transaction and verifies all of its attributes.
    function validateWarpMessage(
        uint32 index,
        bytes32 sourceChainID,
        address originSenderAddress,
        bytes calldata payload
    ) external view {
        (WarpMessage memory message, bool valid) = warp.getVerifiedWarpMessage(
            index
        );
        require(valid);
        require(message.sourceChainID == sourceChainID);
        require(message.originSenderAddress == originSenderAddress);
        require(keccak256(message.payload) == keccak256(payload));
    }

    function validateInvalidWarpMessage(uint32 index) external view {
        (WarpMessage memory message, bool valid) = warp.getVerifiedWarpMessage(
            index
        );
        require(!valid);
        require(message.sourceChainID == bytes32(0));
        require(message.originSenderAddress == address(0));
        require(keccak256(message.payload) == keccak256(bytes("")));
    }

    // validateWarpBlockHash retrieves the warp block hash attached to the transaction and verifies it matches the
    // expected block hash.
    function validateWarpBlockHash(
        uint32 index,
        bytes32 sourceChainID,
        bytes32 blockHash
    ) external view {
        (WarpBlockHash memory warpBlockHash, bool valid) = warp
            .getVerifiedWarpBlockHash(index);
        require(valid);
        require(warpBlockHash.sourceChainID == sourceChainID);
        require(warpBlockHash.blockHash == blockHash);
    }

    function validateInvalidWarpBlockHash(uint32 index) external view {
        (WarpBlockHash memory warpBlockHash, bool valid) = warp
            .getVerifiedWarpBlockHash(index);
        require(!valid);
        require(warpBlockHash.sourceChainID == bytes32(0));
        require(warpBlockHash.blockHash == bytes32(0));
    }

    // validateGetBlockchainID checks that the blockchainID returned by warp matches the argument
    function validateGetBlockchainID(bytes32 blockchainID) external view {
        require(blockchainID == warp.getBlockchainID());
    }
}
