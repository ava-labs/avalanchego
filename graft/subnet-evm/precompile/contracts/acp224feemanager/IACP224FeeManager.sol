//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "precompile/allowlist/IAllowList.sol";

/// @title ACP-224 Fee Manager Interface
/// @notice Interface for managing dynamic gas limit and fee parameters
/// @dev Inherits from IAllowList for access control
interface IACP224FeeManager is IAllowList {
    /// @notice Configuration parameters for the dynamic fee mechanism
    struct FeeConfig {
        uint256 targetGas; // Target gas consumption per second
        uint256 minGasPrice; // Minimum gas price in wei
        uint256 maxCapacityFactor; // Maximum capacity factor (C = factor * T)
        uint256 timeToDouble; // Time in seconds for gas price to double at max capacity
    }

    /// @notice Emitted when fee configuration is updated
    /// @param sender Address that triggered the update
    /// @param oldFeeConfig Previous configuration
    /// @param newFeeConfig New configuration
    event FeeConfigUpdated(
        address indexed sender,
        FeeConfig oldFeeConfig,
        FeeConfig newFeeConfig
    );

    /// @notice Set the fee configuration
    /// @dev Only callable by addresses with Admin or Manager role
    /// @param config New fee configuration parameters
    function setFeeConfig(FeeConfig calldata config) external;

    /// @notice Get the current fee configuration
    /// @return config Current fee configuration
    function getFeeConfig() external view returns (FeeConfig memory config);

    /// @notice Get the block number when fee config was last changed
    /// @return blockNumber Block number of last configuration change
    function getFeeConfigLastChangedAt()
        external
        view
        returns (uint256 blockNumber);
}
