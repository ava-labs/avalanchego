pragma solidity >=0.6.0;

library MultiCoin {
    function transfer(address payable recipient, uint256 amount, uint256 coinid, uint256 amount2) public {
        recipient.transferex(amount, coinid, amount2);
    }

    function getBalance(uint256 coinid) public view returns (uint256) {
        return address(this).balancemc(coinid);
    }
}
