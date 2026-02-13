//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;
import "precompile/allowlist/IAllowList.sol";

interface IRewardManager is IAllowList {
    // RewardAddressChanged is the event logged whenever reward address is modified
    event RewardAddressChanged(
        address indexed sender,
        address indexed oldRewardAddress,
        address indexed newRewardAddress
    );

    // FeeRecipientsAllowed is the event logged whenever fee recipient is modified
    event FeeRecipientsAllowed(address indexed sender);

    // RewardsDisabled is the event logged whenever rewards are disabled
    event RewardsDisabled(address indexed sender);

    // setRewardAddress sets the reward address to the given address
    function setRewardAddress(address addr) external;

    // allowFeeRecipients allows block builders to claim fees
    function allowFeeRecipients() external;

    // disableRewards disables block rewards and starts burning fees
    function disableRewards() external;

    // currentRewardAddress returns the current reward address
    function currentRewardAddress()
        external
        view
        returns (address rewardAddress);

    // areFeeRecipientsAllowed returns true if fee recipients are allowed
    function areFeeRecipientsAllowed() external view returns (bool isAllowed);
}
