// SPDX-License-Identifier: BSD-3-Clause
pragma solidity ^0.8.28;

interface IWarpMessenger {
    function sendWarpMessage(bytes calldata payload) external returns (bytes32 messageID);
    function getBlockchainID() external view returns (bytes32 blockchainID);
}

/// Moves AVAX between the C-chain and the P-chain for any EVM wallet with
/// ordinary EVM transactions. The C-chain trusts this contract's address to
/// name msg.sender in the warp messages it emits; nothing here holds a key.
contract CChainHelper {
    IWarpMessenger private constant WARP = IWarpMessenger(0x0200000000000000000000000000000000000005);

    uint16 private constant CODEC_VERSION = 0;
    uint32 private constant TYPE_TRANSFER_INPUT = 5;
    /// C-chain atomic tx codec (vms/saevm/cchain/tx).
    uint32 private constant C_TYPE_IMPORT = 0;

    uint32 public immutable networkID;
    bytes32 public immutable avaxAssetID;

    struct UTXO {
        bytes32 txID;
        uint32 outputIndex;
        uint64 amount;
    }

    error BadAmount();
    error InputsNotSorted();

    constructor(uint32 networkID_, bytes32 avaxAssetID_) {
        networkID = networkID_;
        avaxAssetID = avaxAssetID_;
    }

    /// Exports msg.value (whole nAVAX) to the P-chain as a UTXO owned by [to],
    /// any 20-byte P-chain address. The AVAX stays here until the SAE hook
    /// reads the warp log (to || nAVAX), debits this contract and writes the
    /// UTXO into shared memory.
    function exportToP(address to) external payable returns (bytes32) {
        if (msg.value == 0 || msg.value % 1e9 != 0) revert BadAmount();
        return WARP.sendWarpMessage(abi.encodePacked(to, uint64(msg.value / 1e9)));
    }

    /// Imports [imported], UTXOs owned by msg.sender waiting in shared memory
    /// from the P-chain, into msg.sender's C-chain balance; [fee] nAVAX is
    /// burned. Emits the exact C-chain ImportTx bytes prefixed with
    /// msg.sender and the emission height; anyone may then issue that tx with
    /// this message as its credential. Callers pass [imported] sorted.
    function importFromP(UTXO[] calldata imported, uint64 fee) external returns (bytes32) {
        uint64 total;
        bytes memory ins = abi.encodePacked(uint32(imported.length));
        for (uint256 i = 0; i < imported.length; i++) {
            if (i > 0 && !before(imported[i - 1], imported[i])) revert InputsNotSorted();
            total += imported[i].amount;
            ins = abi.encodePacked(
                ins, imported[i].txID, imported[i].outputIndex, avaxAssetID, TYPE_TRANSFER_INPUT, imported[i].amount, uint32(1), uint32(0)
            );
        }
        if (total <= fee) revert BadAmount();
        bytes memory tx_ = abi.encodePacked(
            CODEC_VERSION, C_TYPE_IMPORT, networkID, WARP.getBlockchainID(), bytes32(0), ins, uint32(1), msg.sender, total - fee, avaxAssetID
        );
        return WARP.sendWarpMessage(abi.encodePacked(msg.sender, uint64(block.number), tx_));
    }

    function before(UTXO calldata a, UTXO calldata b) private pure returns (bool) {
        if (a.txID != b.txID) return uint256(a.txID) < uint256(b.txID);
        return a.outputIndex < b.outputIndex;
    }
}
