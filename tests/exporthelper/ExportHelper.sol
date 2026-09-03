// SPDX-License-Identifier: BSD-3-Clause
pragma solidity ^0.8.28;

interface IWarpMessenger {
    function sendWarpMessage(bytes calldata payload) external returns (bytes32 messageID);
}

/// Exports AVAX from the C-chain to a P-chain owner with one ordinary EVM tx.
/// The C-chain trusts this contract's address: after the block executes, the
/// SAE hook reads the warp log (owner || nAVAX), debits this contract and
/// writes the UTXO into shared memory. Nobody signs anything Avalanche-specific.
contract ExportHelper {
    IWarpMessenger private constant WARP = IWarpMessenger(0x0200000000000000000000000000000000000005);

    error BadAmount();

    /// Moves msg.value (whole nAVAX) to the P-chain as a UTXO owned by [to],
    /// any 20-byte P-chain address. The AVAX stays here until the hook
    /// debits it.
    function exportToP(address to) external payable returns (bytes32) {
        if (msg.value == 0 || msg.value % 1e9 != 0) revert BadAmount();
        return WARP.sendWarpMessage(abi.encodePacked(to, uint64(msg.value / 1e9)));
    }
}
