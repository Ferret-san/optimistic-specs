//SPDX-License-Identifier: MIT
pragma solidity 0.8.10;

/**
 * @title L1Block
 */
contract L1Block {
    /**
     * Only the Depositor account may call setL1BlockValues().
     */
    error OnlyDepositor();

    address public constant DEPOSITOR_ACCOUNT = 0xDeaDDEaDDeAdDeAdDEAdDEaddeAddEAdDEAd0001;

    uint256 public number;
    uint256 public timestamp;
    uint256 public basefee;
    uint256 public sequenceNumber;
    bytes32 public hash;

    function setL1BlockValues(
        uint256 _number,
        uint256 _timestamp,
        uint256 _basefee,
        uint256 _sequenceNumber,
        bytes32 _hash
    ) external {
        if (msg.sender != DEPOSITOR_ACCOUNT) {
            revert OnlyDepositor();
        }

        number = _number;
        timestamp = _timestamp;
        basefee = _basefee;
        sequenceNumber = _sequenceNumber;
        hash = _hash;
    }
}
