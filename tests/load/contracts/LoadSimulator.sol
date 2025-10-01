// SPDX-License-Identifier: BSD 3-Clause
pragma solidity ^0.8.21;

import {Dummy} from "./Dummy.sol";

/**
 * @dev LoadSimulator is a smart contract designed for load testing.
 *
 * This contract provides a suite of methods to simulate various intensive blockchain
 * operations with documented gas costs and calldata lengths, making it ideal for
 * performance testing with specific targets.
 *
 * LoadSimulator includes methods that focus on computationally intensive operations.
 * This includes database read/writes and hashing.
 */
contract LoadSimulator {
    // Storage slot 0
    Dummy dummy;
    // Storage slot 1
    uint256 latestEmptySlot;

    event LargeCalldata(bytes);

    constructor() {
        latestEmptySlot = 2;
        dummy = new Dummy();
    }

    /**
     * @dev Read numSlots starting at offset.
     *
     * Calldata Length: 4 + 32 + 32 = 68 bytes
     * Minimum Gas Used: 21_000 + (2100 * numSlots)
     */
    function read(
        uint256 offset,
        uint256 numSlots
    ) external returns (uint256 sum) {
        assembly {
            let newOffset := add(offset, numSlots)
            for {
                let i := offset
            } lt(i, newOffset) {
                i := add(i, 1)
            } {
                sum := add(sum, sload(i))
            }
        }
    }

    /**
     * @dev Write value to numSlots whose slot value is empty.
     *
     * Calldata Length: 4 + 32 + 32 = 68 bytes
     * Minimum Gas Used: 21_000 + (22_100 * numSlots)
     */
    function write(uint256 numSlots, uint256 value) external {
        assembly {
            let offset := sload(latestEmptySlot.slot)
            let newOffset := add(offset, numSlots)
            for {
                let i := offset
            } lt(i, newOffset) {
                i := add(i, 1)
            } {
                sstore(i, value)
            }
            sstore(latestEmptySlot.slot, newOffset)
        }
    }

    /**
     * @dev Modify numSlots whose slot value is non-empty.
     *
     * Calldata Length: 4 + 32 + 32 = 68 bytes
     * Minimum Gas Used: 21_000 + (5_000 * numSlots)
     */
    function modify(
        uint256 numSlots,
        uint256 newValue
    ) external returns (bool success) {
        assembly {
            let offset := 2
            let firstEmptySlot := sload(latestEmptySlot.slot)
            let numModifiedSlots := sub(firstEmptySlot, offset)

            // if numModifiedSlots >= numSlots
            if not(lt(numModifiedSlots, numSlots)) {
                for {
                    let i := offset
                } lt(i, add(offset, numSlots)) {
                    i := add(i, 1)
                } {
                    sstore(i, newValue)
                }
                success := true
            }
        }
    }

    /**
     * @dev Hash value for n iterations.
     *
     * Calldata: 4 + 32 + 32 = 68 bytes
     * Minimum Gas Used: 21_000 + (30 * n)
     */
    function hash(uint256 value, uint256 n) external returns (bytes32 result) {
        result = bytes32(value);
        for (uint256 i = 0; i < n; i++) {
            result = keccak256(abi.encode(result));
        }
    }

    /**
     * @dev Deploy an instance of the Dummy contract.
     *
     * Calldata: 4 bytes
     * Minimum Gas Used: 290_000
     */
    function deploy() external {
        dummy = new Dummy();
    }

    /**
     * @dev Computes the sum of an arbitary-length byte-array
     * This function is used to test transactions with large calldata lengths.
     *
     * Calldata: 4 + 32 + 32 + L
     * where L = data.length rounded up to the nearest multiple of 32.
     *
     * Minimum Gas Used: 23_000
     */
    function largeCalldata(bytes calldata data) external {
        emit LargeCalldata(data);
    }
}
