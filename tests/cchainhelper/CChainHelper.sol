// SPDX-License-Identifier: BSD-3-Clause
pragma solidity ^0.8.28;

interface IWarpMessenger {
    function sendWarpMessage(bytes calldata payload) external returns (bytes32 messageID);
    function getBlockchainID() external view returns (bytes32 blockchainID);
}

/// Moves AVAX between the C-chain and the P-chain for any EVM wallet with
/// ordinary EVM transactions. The C-chain trusts this contract to bind import
/// approvals and export messages to the caller.
contract CChainHelper {
    IWarpMessenger private constant WARP = IWarpMessenger(0x0200000000000000000000000000000000000005);

    uint16 private constant CODEC_VERSION = 0;
    uint32 private constant TYPE_TRANSFER_INPUT = 5;
    /// C-chain atomic tx codec (vms/saevm/cchain/tx).
    uint32 private constant C_TYPE_IMPORT = 0;

    // Consensus reads this mapping directly. Keep it at storage slot 0.
    mapping(bytes32 => bool) public authorized;

    event ImportAuthorized(bytes32 indexed importHash, bytes unsignedTx);

    struct UTXO {
        bytes32 txID;
        uint32 outputIndex;
        uint64 amount;
    }

    error BadAmount();
    error InputsNotSorted();

    /// Exports msg.value (whole nAVAX) to the P-chain as a UTXO owned by [to],
    /// any 20-byte P-chain address. The AVAX stays here until the SAE hook
    /// reads the warp log (to || nAVAX), debits this contract and writes the
    /// UTXO into shared memory.
    function exportToP(address to) external payable returns (bytes32) {
        if (msg.value == 0 || msg.value % 1e9 != 0 || msg.value / 1e9 > type(uint64).max) revert BadAmount();
        return WARP.sendWarpMessage(abi.encodePacked(to, uint64(msg.value / 1e9)));
    }

    /// Authorizes an import of [imported] to msg.sender with [fee] nAVAX burned.
    /// Anyone can submit the emitted ImportTx bytes with empty credentials.
    /// The atomic verifier checks ownership and availability of the UTXOs.
    /// Callers pass [imported] sorted and the network ID and AVAX asset ID of
    /// the chain; wrong values fail the atomic verifier. This call does not
    /// complete the import.
    function importFromP(uint32 networkID, bytes32 avaxAssetID, UTXO[] calldata imported, uint64 fee)
        external
        returns (bytes32)
    {
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
        bytes memory tx_ = abi.encodePacked(CODEC_VERSION, C_TYPE_IMPORT, networkID, WARP.getBlockchainID(), bytes32(0), ins);
        tx_ = abi.encodePacked(tx_, uint32(1), msg.sender, total - fee, avaxAssetID);
        bytes32 importHash = keccak256(tx_);
        authorized[importHash] = true;
        emit ImportAuthorized(importHash, tx_);
        return importHash;
    }

    function before(UTXO calldata a, UTXO calldata b) private pure returns (bool) {
        if (a.txID != b.txID) return uint256(a.txID) < uint256(b.txID);
        return a.outputIndex < b.outputIndex;
    }
}
