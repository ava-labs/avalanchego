//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "./IRewardManager.sol";

contract RewardManagerTest {
    IRewardManager private rewardManager;

    constructor(address rewardManagerPrecompile) {
        rewardManager = IRewardManager(rewardManagerPrecompile);
    }

    function setRewardAddress(address addr) external {
        rewardManager.setRewardAddress(addr);
    }

    function allowFeeRecipients() external {
        rewardManager.allowFeeRecipients();
    }

    function disableRewards() external {
        rewardManager.disableRewards();
    }

    function currentRewardAddress() external view returns (address) {
        return rewardManager.currentRewardAddress();
    }

    function areFeeRecipientsAllowed() external view returns (bool) {
        return rewardManager.areFeeRecipientsAllowed();
    }

    // Allow contract to receive ETH for fee testing
    receive() external payable {}
}
