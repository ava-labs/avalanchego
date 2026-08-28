// SPDX-License-Identifier: BSD-3-Clause
pragma solidity ^0.8.28;

interface IWarpMessenger {
    function sendWarpMessage(bytes calldata payload) external returns (bytes32 messageID);
    function getBlockchainID() external view returns (bytes32 blockchainID);
}

/// Builds P-chain transactions owned by msg.sender and ships them over warp.
/// The P-chain trusts this contract's address to name the owner in the
/// payload (owner || unsigned tx bytes). Every input is an AVAX UTXO owned
/// solely by msg.sender; [change] AVAX goes back to msg.sender and the rest
/// of the inputs is burned as the fee. Callers pass inputs, outputs and
/// owner addresses already sorted; the contract only checks the order.
contract PChain {
    IWarpMessenger private constant WARP = IWarpMessenger(0x0200000000000000000000000000000000000005);

    uint16 private constant CODEC_VERSION = 0;
    uint32 private constant TYPE_TRANSFER_INPUT = 5;
    uint32 private constant TYPE_TRANSFER_OUTPUT = 7;
    uint32 private constant TYPE_INPUT = 10;
    uint32 private constant TYPE_OUTPUT_OWNERS = 11;
    uint32 private constant TYPE_ADD_SUBNET_VALIDATOR = 13;
    uint32 private constant TYPE_CREATE_CHAIN = 15;
    uint32 private constant TYPE_CREATE_SUBNET = 16;
    uint32 private constant TYPE_IMPORT = 17;
    uint32 private constant TYPE_EXPORT = 18;
    uint32 private constant TYPE_REMOVE_SUBNET_VALIDATOR = 23;
    uint32 private constant TYPE_ADD_PERMISSIONLESS_VALIDATOR = 25;
    uint32 private constant TYPE_ADD_PERMISSIONLESS_DELEGATOR = 26;
    uint32 private constant TYPE_SIGNER_EMPTY = 27;
    uint32 private constant TYPE_SIGNER_POP = 28;
    uint32 private constant TYPE_TRANSFER_SUBNET_OWNERSHIP = 33;
    uint32 private constant TYPE_BASE = 34;
    uint32 private constant TYPE_CONVERT_SUBNET_TO_L1 = 35;
    uint32 private constant TYPE_REGISTER_L1_VALIDATOR = 36;
    uint32 private constant TYPE_SET_L1_VALIDATOR_WEIGHT = 37;
    uint32 private constant TYPE_INCREASE_L1_VALIDATOR_BALANCE = 38;
    uint32 private constant TYPE_DISABLE_L1_VALIDATOR = 39;
    uint32 private constant TYPE_ADD_AUTO_RENEWED_VALIDATOR = 40;
    uint32 private constant TYPE_SET_AUTO_RENEWED_VALIDATOR_CONFIG = 41;
    /// C-chain atomic tx codec (vms/saevm/cchain/tx).
    uint32 private constant C_TYPE_IMPORT = 0;

    uint32 public immutable networkID;
    bytes32 public immutable pChainID;
    bytes32 public immutable avaxAssetID;

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

    /// An AVAX transferable output.
    struct Out {
        uint64 amount;
        Owners owners;
    }

    struct Validator {
        bytes20 nodeID;
        uint64 start;
        uint64 end;
        uint64 weight;
    }

    /// BLS public key (48 bytes) and proof of possession (96 bytes). An empty
    /// public key means no signer.
    struct BLS {
        bytes publicKey;
        bytes proofOfPossession;
    }

    struct PChainOwner {
        uint32 threshold;
        address[] addrs;
    }

    struct L1Validator {
        bytes nodeID;
        uint64 weight;
        uint64 balance;
        BLS bls;
        PChainOwner remainingBalanceOwner;
        PChainOwner deactivationOwner;
    }

    struct Staking {
        Validator validator;
        bytes32 subnetID;
        BLS bls;
        Out[] stake;
        Owners validatorRewards;
        Owners delegatorRewards;
        uint32 delegationShares;
    }

    struct AutoRenewed {
        bytes nodeID;
        BLS bls;
        Out[] stake;
        Owners validatorRewards;
        Owners delegatorRewards;
        Owners validatorAuthority;
        uint32 delegationShares;
        uint32 autoCompoundRewardShares;
        uint64 period;
    }

    error InputsNotSorted();
    error OutputsNotSorted();
    error OwnersNotSorted();
    error BadThreshold();
    error BadLength();
    error BadAmount();

    constructor(uint32 networkID_, bytes32 pChainID_, bytes32 avaxAssetID_) {
        networkID = networkID_;
        pChainID = pChainID_;
        avaxAssetID = avaxAssetID_;
    }

    // ---- transactions -------------------------------------------------

    function transfer(UTXO[] calldata ins, Out[] calldata outs) external returns (bytes32) {
        return send(abi.encodePacked(header(TYPE_BASE), encodeOuts(outs), encodeInputs(ins), uint32(0)));
    }

    function createSubnet(UTXO[] calldata ins, uint64 change, Owners calldata owner) external returns (bytes32) {
        return send(abi.encodePacked(base(TYPE_CREATE_SUBNET, ins, change), encodeOwners(owner)));
    }

    function createChain(
        UTXO[] calldata ins,
        uint64 change,
        bytes32 subnetID,
        string calldata chainName,
        bytes32 vmID,
        bytes32[] calldata fxIDs,
        bytes calldata genesis,
        uint32[] calldata subnetAuth
    ) external returns (bytes32) {
        bytes memory tail = abi.encodePacked(
            vmID, uint32(fxIDs.length), abi.encodePacked(fxIDs), encodeBytes(genesis), encodeAuth(subnetAuth)
        );
        return send(
            abi.encodePacked(
                base(TYPE_CREATE_CHAIN, ins, change), subnetID, uint16(bytes(chainName).length), chainName, tail
            )
        );
    }

    function addSubnetValidator(
        UTXO[] calldata ins,
        uint64 change,
        Validator calldata validator,
        bytes32 subnetID,
        uint32[] calldata subnetAuth
    ) external returns (bytes32) {
        return send(
            abi.encodePacked(
                base(TYPE_ADD_SUBNET_VALIDATOR, ins, change), encodeValidator(validator), subnetID, encodeAuth(subnetAuth)
            )
        );
    }

    function removeSubnetValidator(
        UTXO[] calldata ins,
        uint64 change,
        bytes20 nodeID,
        bytes32 subnetID,
        uint32[] calldata subnetAuth
    ) external returns (bytes32) {
        return send(
            abi.encodePacked(base(TYPE_REMOVE_SUBNET_VALIDATOR, ins, change), nodeID, subnetID, encodeAuth(subnetAuth))
        );
    }

    function addPermissionlessValidator(UTXO[] calldata ins, uint64 change, Staking calldata s)
        external
        returns (bytes32)
    {
        return send(
            abi.encodePacked(
                base(TYPE_ADD_PERMISSIONLESS_VALIDATOR, ins, change),
                encodeValidator(s.validator),
                s.subnetID,
                encodeSigner(s.bls),
                encodeOuts(s.stake),
                encodeOwners(s.validatorRewards),
                encodeOwners(s.delegatorRewards),
                s.delegationShares
            )
        );
    }

    function addPermissionlessDelegator(
        UTXO[] calldata ins,
        uint64 change,
        Validator calldata validator,
        bytes32 subnetID,
        Out[] calldata stake,
        Owners calldata rewards
    ) external returns (bytes32) {
        return send(
            abi.encodePacked(
                base(TYPE_ADD_PERMISSIONLESS_DELEGATOR, ins, change),
                encodeValidator(validator),
                subnetID,
                encodeOuts(stake),
                encodeOwners(rewards)
            )
        );
    }

    function transferSubnetOwnership(
        UTXO[] calldata ins,
        uint64 change,
        bytes32 subnetID,
        uint32[] calldata subnetAuth,
        Owners calldata newOwner
    ) external returns (bytes32) {
        return send(
            abi.encodePacked(
                base(TYPE_TRANSFER_SUBNET_OWNERSHIP, ins, change), subnetID, encodeAuth(subnetAuth), encodeOwners(newOwner)
            )
        );
    }

    /// [imported] are the UTXOs waiting in shared memory from [sourceChain].
    function importTx(UTXO[] calldata ins, uint64 change, bytes32 sourceChain, UTXO[] calldata imported)
        external
        returns (bytes32)
    {
        return send(abi.encodePacked(base(TYPE_IMPORT, ins, change), sourceChain, encodeInputs(imported)));
    }

    function exportTx(UTXO[] calldata ins, uint64 change, bytes32 destinationChain, Out[] calldata exported)
        external
        returns (bytes32)
    {
        return send(abi.encodePacked(base(TYPE_EXPORT, ins, change), destinationChain, encodeOuts(exported)));
    }

    function convertSubnetToL1(
        UTXO[] calldata ins,
        uint64 change,
        bytes32 subnetID,
        bytes32 chainID,
        bytes calldata managerAddress,
        L1Validator[] calldata validators,
        uint32[] calldata subnetAuth
    ) external returns (bytes32) {
        bytes memory vdrs = abi.encodePacked(uint32(validators.length));
        for (uint256 i = 0; i < validators.length; i++) {
            L1Validator calldata v = validators[i];
            if (v.bls.publicKey.length != 48 || v.bls.proofOfPossession.length != 96) revert BadLength();
            vdrs = abi.encodePacked(
                vdrs,
                encodeBytes(v.nodeID),
                v.weight,
                v.balance,
                v.bls.publicKey,
                v.bls.proofOfPossession,
                encodePChainOwner(v.remainingBalanceOwner),
                encodePChainOwner(v.deactivationOwner)
            );
        }
        return send(
            abi.encodePacked(
                base(TYPE_CONVERT_SUBNET_TO_L1, ins, change),
                subnetID,
                chainID,
                encodeBytes(managerAddress),
                vdrs,
                encodeAuth(subnetAuth)
            )
        );
    }

    function registerL1Validator(
        UTXO[] calldata ins,
        uint64 change,
        uint64 balance,
        bytes calldata proofOfPossession,
        bytes calldata message
    ) external returns (bytes32) {
        if (proofOfPossession.length != 96) revert BadLength();
        return send(
            abi.encodePacked(
                base(TYPE_REGISTER_L1_VALIDATOR, ins, change), balance, proofOfPossession, encodeBytes(message)
            )
        );
    }

    function setL1ValidatorWeight(UTXO[] calldata ins, uint64 change, bytes calldata message)
        external
        returns (bytes32)
    {
        return send(abi.encodePacked(base(TYPE_SET_L1_VALIDATOR_WEIGHT, ins, change), encodeBytes(message)));
    }

    function increaseL1ValidatorBalance(UTXO[] calldata ins, uint64 change, bytes32 validationID, uint64 balance)
        external
        returns (bytes32)
    {
        return send(abi.encodePacked(base(TYPE_INCREASE_L1_VALIDATOR_BALANCE, ins, change), validationID, balance));
    }

    function disableL1Validator(
        UTXO[] calldata ins,
        uint64 change,
        bytes32 validationID,
        uint32[] calldata disableAuth
    ) external returns (bytes32) {
        return send(
            abi.encodePacked(base(TYPE_DISABLE_L1_VALIDATOR, ins, change), validationID, encodeAuth(disableAuth))
        );
    }

    function addAutoRenewedValidator(UTXO[] calldata ins, uint64 change, AutoRenewed calldata a)
        external
        returns (bytes32)
    {
        bytes memory tail = abi.encodePacked(
            encodeOwners(a.validatorRewards),
            encodeOwners(a.delegatorRewards),
            encodeOwners(a.validatorAuthority),
            a.delegationShares,
            a.autoCompoundRewardShares,
            a.period
        );
        return send(
            abi.encodePacked(
                base(TYPE_ADD_AUTO_RENEWED_VALIDATOR, ins, change),
                encodeBytes(a.nodeID),
                encodeSigner(a.bls),
                encodeOuts(a.stake),
                tail
            )
        );
    }

    function setAutoRenewedValidatorConfig(
        UTXO[] calldata ins,
        uint64 change,
        bytes32 txID,
        uint32[] calldata auth,
        uint32 autoCompoundRewardShares,
        uint64 period
    ) external returns (bytes32) {
        return send(
            abi.encodePacked(
                base(TYPE_SET_AUTO_RENEWED_VALIDATOR_CONFIG, ins, change),
                txID,
                encodeAuth(auth),
                autoCompoundRewardShares,
                period
            )
        );
    }

    // ---- C-chain boundary ---------------------------------------------

    /// Moves msg.value (whole nAVAX) to the P-chain as a UTXO owned by
    /// msg.sender. The AVAX stays in this contract until the C-chain
    /// executes the message, debits it and writes the UTXO to shared memory.
    function exportToP() external payable returns (bytes32) {
        if (msg.value == 0 || msg.value % 1e9 != 0) revert BadAmount();
        return WARP.sendWarpMessage(abi.encodePacked(msg.sender, uint64(msg.value / 1e9)));
    }

    /// Imports [imported], UTXOs owned by msg.sender waiting in shared memory
    /// from the P-chain, into msg.sender's C-chain balance; [fee] nAVAX is
    /// burned. Encodes a C-chain ImportTx, not a P-chain tx.
    function importFromP(UTXO[] calldata imported, uint64 fee) external returns (bytes32) {
        uint64 total;
        for (uint256 i = 0; i < imported.length; i++) {
            total += imported[i].amount;
        }
        if (total <= fee) revert BadAmount();
        return send(
            abi.encodePacked(
                CODEC_VERSION,
                C_TYPE_IMPORT,
                networkID,
                WARP.getBlockchainID(),
                pChainID,
                encodeInputs(imported),
                uint32(1),
                msg.sender,
                total - fee,
                avaxAssetID
            )
        );
    }

    // ---- encoding -----------------------------------------------------

    function send(bytes memory tx_) private returns (bytes32) {
        return WARP.sendWarpMessage(abi.encodePacked(msg.sender, tx_));
    }

    function header(uint32 typeID) private view returns (bytes memory) {
        return abi.encodePacked(CODEC_VERSION, typeID, networkID, pChainID);
    }

    /// Tx header, change output to msg.sender, inputs, empty memo.
    function base(uint32 typeID, UTXO[] calldata ins, uint64 change) private view returns (bytes memory) {
        bytes memory outs = change == 0
            ? abi.encodePacked(uint32(0))
            : abi.encodePacked(uint32(1), encodeOut(change, 0, 1, msg.sender));
        return abi.encodePacked(header(typeID), outs, encodeInputs(ins), uint32(0));
    }

    function encodeOut(uint64 amount, uint64 locktime, uint32 threshold, address addr)
        private
        view
        returns (bytes memory)
    {
        return abi.encodePacked(avaxAssetID, TYPE_TRANSFER_OUTPUT, amount, locktime, threshold, uint32(1), addr);
    }

    function encodeOuts(Out[] calldata outs) private view returns (bytes memory out) {
        out = abi.encodePacked(uint32(outs.length));
        bytes memory prev;
        for (uint256 i = 0; i < outs.length; i++) {
            bytes memory cur = abi.encodePacked(avaxAssetID, TYPE_TRANSFER_OUTPUT, outs[i].amount, ownersBody(outs[i].owners));
            if (i > 0 && !lessBytes(prev, cur)) revert OutputsNotSorted();
            out = abi.encodePacked(out, cur);
            prev = cur;
        }
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

    function lessBytes(bytes memory a, bytes memory b) private pure returns (bool) {
        uint256 n = a.length < b.length ? a.length : b.length;
        for (uint256 i = 0; i < n; i++) {
            if (a[i] != b[i]) return a[i] < b[i];
        }
        return a.length < b.length;
    }

    function encodeOwners(Owners calldata o) private pure returns (bytes memory) {
        return abi.encodePacked(TYPE_OUTPUT_OWNERS, ownersBody(o));
    }

    function ownersBody(Owners calldata o) private pure returns (bytes memory) {
        return abi.encodePacked(o.locktime, o.threshold, encodeAddrs(o.threshold, o.addrs));
    }

    function encodePChainOwner(PChainOwner calldata o) private pure returns (bytes memory) {
        return abi.encodePacked(o.threshold, encodeAddrs(o.threshold, o.addrs));
    }

    function encodeAddrs(uint32 threshold, address[] calldata addrs) private pure returns (bytes memory out) {
        if (threshold > addrs.length) revert BadThreshold();
        out = abi.encodePacked(uint32(addrs.length));
        for (uint256 i = 0; i < addrs.length; i++) {
            if (i > 0 && uint160(addrs[i - 1]) >= uint160(addrs[i])) revert OwnersNotSorted();
            out = abi.encodePacked(out, addrs[i]);
        }
    }

    function encodeAuth(uint32[] calldata sigIndices) private pure returns (bytes memory out) {
        out = abi.encodePacked(TYPE_INPUT, uint32(sigIndices.length));
        for (uint256 i = 0; i < sigIndices.length; i++) {
            out = abi.encodePacked(out, sigIndices[i]);
        }
    }

    function encodeValidator(Validator calldata v) private pure returns (bytes memory) {
        return abi.encodePacked(v.nodeID, v.start, v.end, v.weight);
    }

    function encodeSigner(BLS calldata b) private pure returns (bytes memory) {
        if (b.publicKey.length == 0) return abi.encodePacked(TYPE_SIGNER_EMPTY);
        if (b.publicKey.length != 48 || b.proofOfPossession.length != 96) revert BadLength();
        return abi.encodePacked(TYPE_SIGNER_POP, b.publicKey, b.proofOfPossession);
    }

    function encodeBytes(bytes calldata b) private pure returns (bytes memory) {
        return abi.encodePacked(uint32(b.length), b);
    }
}
