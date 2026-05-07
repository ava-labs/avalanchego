//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "precompile/allowlist/IAllowList.sol";

/// @title Gas Price Manager Interface
/// @notice Interface for managing dynamic gas limit and gas price parameters
/// @dev Inherits from IAllowList for access control
interface IGasPriceManager is IAllowList {
    /// @notice Configuration parameters for the dynamic gas price mechanism
    /// @dev Fields are ordered so each mode flag precedes the parameter(s) it governs,
    ///      reducing the risk of mis-ordering arguments.
    struct GasPriceConfig {
        bool    validatorTargetGas; // When true, validators control targetGas via node preferences
        uint64 targetGas;           // Target gas consumption per second (T)
        bool    staticPricing;      // When true, gas price is always minGasPrice
        uint64 minGasPrice;         // Minimum gas price in wei (M)
        uint64 timeToDouble;        // Seconds for gas price to double at max capacity
    }

    /// @notice Emitted when the gas price configuration is updated
    /// @param sender Address that triggered the update
    /// @param oldGasPriceConfig Previous configuration
    /// @param newGasPriceConfig New configuration
    event GasPriceConfigUpdated(
        address indexed sender,
        GasPriceConfig oldGasPriceConfig,
        GasPriceConfig newGasPriceConfig
    );

    /// @notice Set the gas price configuration
    /// @param config New gas price configuration parameters
    function setGasPriceConfig(GasPriceConfig calldata config) external;

    /// @notice Get the current gas price configuration
    /// @return config Current gas price configuration
    function getGasPriceConfig() external view returns (GasPriceConfig memory config);

    /// @notice Get the block number when the gas price config was last changed
    /// @return blockNumber Block number of last configuration change
    function getGasPriceConfigLastChangedAt()
        external
        view
        returns (uint256 blockNumber);
}
