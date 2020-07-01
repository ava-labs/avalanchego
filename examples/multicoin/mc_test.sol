pragma solidity >=0.6.0;

contract MCTest {
    address constant MultiCoin = 0x0100000000000000000000000000000000000000;
    constructor() public {
        // enable multi-coin functionality (it is disabled by default)
        (bool success,) = MultiCoin.delegatecall(abi.encodeWithSignature("enableMultiCoin()"));
        require(success);
    }

    function getBalance(uint256 coinid) public returns (uint256) {
        (bool success, bytes memory data) = MultiCoin.delegatecall(abi.encodeWithSignature("getBalance(uint256)", coinid));
        require(success);
        return abi.decode(data, (uint256));
    }

    function deposit() public payable {}
}
