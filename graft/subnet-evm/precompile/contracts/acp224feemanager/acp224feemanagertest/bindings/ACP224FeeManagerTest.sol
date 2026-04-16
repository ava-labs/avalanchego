//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "precompile/contracts/acp224feemanager/IACP224FeeManager.sol";

contract ACP224FeeManagerTest {
    IACP224FeeManager private feeManager;

    constructor(address feeManagerPrecompile) {
        feeManager = IACP224FeeManager(feeManagerPrecompile);
    }

    function setFeeConfig(IACP224FeeManager.FeeConfig calldata config) external {
        feeManager.setFeeConfig(config);
    }

    function getFeeConfig() external view returns (IACP224FeeManager.FeeConfig memory) {
        return feeManager.getFeeConfig();
    }

    function getFeeConfigLastChangedAt() external view returns (uint256) {
        return feeManager.getFeeConfigLastChangedAt();
    }
}
