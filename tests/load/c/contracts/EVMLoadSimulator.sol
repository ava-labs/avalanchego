// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

contract EVMLoadSimulator {
    // Storage mappings for read/write simulations
    mapping(uint256 => uint256) private balances;
    uint256 public balancesCount;

    // Events for simulating logging overhead
    event StorageUpdate(uint256 indexed accountId, uint256 value);
    event SumCalculated(uint256 sum);
    event HashCalculates(bytes32 hash);
    event MemoryWritten(uint256[] arr);

    // Simulate random storage writes
    function simulateRandomWrite(uint256 count) external {
        for (uint256 i = 1; i <= count; i++) {
            uint256 id = balancesCount++;
            balances[id] = i;
            emit StorageUpdate(id, i);
        }
    }

    // Simulate overwriting existing values or adding new ones
    function simulateModification(uint256 count) external {
        for (uint256 i = 1; i <= count; i++) {
            if (i < balancesCount) {
                uint256 newVal = balances[i] + 1;
                balances[i] = newVal;
                emit StorageUpdate(i, newVal);
            } else {
                uint256 id = balancesCount++;
                balances[id] = i;
                emit StorageUpdate(id, i);
            }
        }
    }

    // Simulate repeated storage reads
    function simulateReads(uint256 count) external returns (uint256 sum) {
        for (uint256 i = 1; i <= count; i++) {
            sum += balances[i];
        }
        emit SumCalculated(sum);
    }

    // Simulate hashing computation (e.g. keccak256)
    function simulateHashing(uint256 rounds) external returns (bytes32 hash) {
        hash = keccak256(abi.encodePacked("initial"));
        for (uint256 i = 0; i < rounds; i++) {
            hash = keccak256(abi.encodePacked(hash, i));
        }
        emit HashCalculates(hash);
    }

    // Simulate dynamic memory allocation and usage
    function simulateMemory(uint256 sizeInWords) external returns (uint256 sum) {
        uint256[] memory arr = new uint256[](sizeInWords);
        for (uint256 i = 0; i < sizeInWords; i++) {
            arr[i] = i;
            sum += arr[i];
        }
        emit MemoryWritten(arr);
    }

    // Simulate deep call stack
    function simulateCallDepth(uint256 depth) external {
        if (depth > 0) {
            this.simulateCallDepth(depth - 1);
        }
    }
}
