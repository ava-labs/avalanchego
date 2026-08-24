// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

// BenchToken is the Solidity baseline: a USDC-shaped ERC-20 with the guards a
// fiat-backed token runs on every transfer (pause switch, blocklist on both
// parties, owner-gated mint). Kept to what the benchmark exercises; approve
// and transferFrom are omitted because no level uses them.
contract BenchToken {
    address public owner;
    bool public paused;
    mapping(address => bool) public blocklisted;
    mapping(address => uint256) public balanceOf;

    event Transfer(address indexed from, address indexed to, uint256 value);

    constructor() {
        owner = msg.sender;
    }

    modifier onlyOwner() {
        require(msg.sender == owner, "not owner");
        _;
    }

    function mint(address to, uint256 amount) external onlyOwner {
        balanceOf[to] += amount;
        emit Transfer(address(0), to, amount);
    }

    function setPaused(bool paused_) external onlyOwner {
        paused = paused_;
    }

    function setBlocklisted(address account, bool blocked) external onlyOwner {
        blocklisted[account] = blocked;
    }

    function transfer(address to, uint256 amount) external returns (bool) {
        require(!paused, "paused");
        require(!blocklisted[msg.sender], "sender blocklisted");
        require(!blocklisted[to], "recipient blocklisted");
        uint256 fromBalance = balanceOf[msg.sender];
        require(fromBalance >= amount, "insufficient balance");
        unchecked {
            balanceOf[msg.sender] = fromBalance - amount;
        }
        balanceOf[to] += amount;
        emit Transfer(msg.sender, to, amount);
        return true;
    }
}
