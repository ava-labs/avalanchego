pragma solidity >=0.6.0;

contract MCTest {
    address constant MultiCoin = 0x0100000000000000000000000000000000000000;
    uint256 balance;
    constructor() public {
        //// enable multi-coin functionality (it is disabled by default)
        //(bool success,) = MultiCoin.call(abi.encodeWithSignature("enableMultiCoin()"));
        //require(success);
    }

    function updateBalance(uint256 coinid) public {
        (bool success, bytes memory data) = MultiCoin.call(abi.encodeWithSignature("getBalance(uint256)", coinid));
        require(success);
        balance = abi.decode(data, (uint256));
    }

    function withdraw(uint256 amount, uint256 coinid, uint256 amount2) public {
        (bool success,) = MultiCoin.call(
            abi.encodeWithSignature("transfer(address,uint256,uint256,uint256)",
                                    msg.sender, amount, coinid, amount2));

        require(success);
    }

    receive() external payable {}
}
