//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "precompile/allowlist/IAllowList.sol";

/// @title ACP-224 Fee Manager Interface
/// @notice Interface for managing dynamic gas limit and fee parameters
/// @dev Inherits from IAllowList for access control
interface IACP224FeeManager is IAllowList {
    /// @notice Configuration parameters for the dynamic fee mechanism
    /// @dev Fields are ordered so each mode flag precedes the parameter(s) it governs,
    ///      reducing the risk of mis-ordering arguments.
    ///      uint256 fields (targetGas, minGasPrice, timeToDouble) MUST fit in uint64.
    ///      Values exceeding uint64 range will be rejected by the precompile.
    struct FeeConfig {
        bool validatorTargetGas; // When true, validators control targetGas via node preferences
        uint256 targetGas; // Target gas consumption per second (T) MUST fit in uint64.
        bool staticPricing; // When true, gas price is always minGasPrice
        uint256 minGasPrice; // Minimum gas price in wei (M) MUST fit in uint64.
        uint256 timeToDouble; // Seconds for gas price to double at max capacity MUST fit in uint64.
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
