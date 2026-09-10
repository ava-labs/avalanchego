//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "precompile/contracts/gaspricemanager/IGasPriceManager.sol";

contract GasPriceManagerTest {
    IGasPriceManager private gasPriceManager;

    constructor(address gasPriceManagerPrecompile) {
        gasPriceManager = IGasPriceManager(gasPriceManagerPrecompile);
    }

    function setGasPriceConfig(IGasPriceManager.GasPriceConfig calldata config) external {
        gasPriceManager.setGasPriceConfig(config);
    }

    function getGasPriceConfig() external view returns (IGasPriceManager.GasPriceConfig memory) {
        return gasPriceManager.getGasPriceConfig();
    }

    function getGasPriceConfigLastChangedAt() external view returns (uint256) {
        return gasPriceManager.getGasPriceConfigLastChangedAt();
    }
}
