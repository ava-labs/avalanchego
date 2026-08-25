// SPDX-License-Identifier: BSD-3-Clause
pragma solidity ^0.8.28;

interface IWarpMessenger {
    function sendWarpMessage(bytes calldata payload) external returns (bytes32 messageID);
}

/// Builds P-chain transactions owned by msg.sender and ships them over warp.
/// The P-chain trusts this contract's address to name the owner in the
/// payload (owner || unsigned tx bytes). Callers pass inputs and owner
/// addresses already sorted; the contract only checks the order.
contract PChain {
    IWarpMessenger private constant WARP = IWarpMessenger(0x0200000000000000000000000000000000000005);

    uint16 private constant CODEC_VERSION = 0;
    uint32 private constant TYPE_TRANSFER_INPUT = 5;
    uint32 private constant TYPE_TRANSFER_OUTPUT = 7;
    uint32 private constant TYPE_OUTPUT_OWNERS = 11;
    uint32 private constant TYPE_CREATE_SUBNET_TX = 16;

    uint32 public immutable networkID;
    bytes32 public immutable pChainID;
    bytes32 public immutable avaxAssetID;

    /// An AVAX UTXO owned solely by msg.sender (signature index 0).
    struct UTXO {
        bytes32 txID;
        uint32 outputIndex;
        uint64 amount;
    }

    struct Owners {
        uint64 locktime;
        uint32 threshold;
        address[] addrs;
    }

    error InputsNotSorted();
    error OwnersNotSorted();
    error BadThreshold();

    constructor(uint32 networkID_, bytes32 pChainID_, bytes32 avaxAssetID_) {
        networkID = networkID_;
        pChainID = pChainID_;
        avaxAssetID = avaxAssetID_;
    }

    /// Consumes [ins], returns [change] AVAX to msg.sender, burns the rest as
    /// the fee, and creates a subnet controlled by [subnetOwner].
    function createSubnet(UTXO[] calldata ins, uint64 change, Owners calldata subnetOwner)
        external
        returns (bytes32 messageID)
    {
        return WARP.sendWarpMessage(abi.encodePacked(msg.sender, encodeCreateSubnet(msg.sender, ins, change, subnetOwner)));
    }

    function encodeCreateSubnet(address owner, UTXO[] calldata ins, uint64 change, Owners calldata subnetOwner)
        public
        view
        returns (bytes memory)
    {
        return abi.encodePacked(
            CODEC_VERSION,
            TYPE_CREATE_SUBNET_TX,
            networkID,
            pChainID,
            encodeChange(owner, change),
            encodeInputs(ins),
            uint32(0), // memo
            encodeOwners(subnetOwner)
        );
    }

    function encodeChange(address owner, uint64 change) private view returns (bytes memory) {
        if (change == 0) {
            return abi.encodePacked(uint32(0));
        }
        return abi.encodePacked(
            uint32(1), avaxAssetID, TYPE_TRANSFER_OUTPUT, change, uint64(0), uint32(1), uint32(1), owner
        );
    }

    function encodeInputs(UTXO[] calldata ins) private view returns (bytes memory out) {
        out = abi.encodePacked(uint32(ins.length));
        for (uint256 i = 0; i < ins.length; i++) {
            if (i > 0 && !before(ins[i - 1], ins[i])) revert InputsNotSorted();
            out = abi.encodePacked(
                out,
                ins[i].txID,
                ins[i].outputIndex,
                avaxAssetID,
                TYPE_TRANSFER_INPUT,
                ins[i].amount,
                uint32(1),
                uint32(0)
            );
        }
    }

    function before(UTXO calldata a, UTXO calldata b) private pure returns (bool) {
        if (a.txID != b.txID) return uint256(a.txID) < uint256(b.txID);
        return a.outputIndex < b.outputIndex;
    }

    function encodeOwners(Owners calldata o) private pure returns (bytes memory out) {
        if (o.threshold > o.addrs.length) revert BadThreshold();
        out = abi.encodePacked(TYPE_OUTPUT_OWNERS, o.locktime, o.threshold, uint32(o.addrs.length));
        for (uint256 i = 0; i < o.addrs.length; i++) {
            if (i > 0 && uint160(o.addrs[i - 1]) >= uint160(o.addrs[i])) revert OwnersNotSorted();
            out = abi.encodePacked(out, o.addrs[i]);
        }
    }
}
