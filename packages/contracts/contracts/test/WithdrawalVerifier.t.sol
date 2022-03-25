pragma solidity 0.8.10;

/* Testing utilities */
import { DSTest } from "../../lib/ds-test/src/test.sol";
import { Vm } from "../../lib/forge-std/src/Vm.sol";
import { L2OutputOracle_Initializer } from "./L2OutputOracle.t.sol";


/* Target contract dependencies */
import { L2OutputOracle } from "../L1/L2OutputOracle.sol";

/* Target contract */
import { WithdrawalVerifier } from "../L1/WithdrawalVerifier.sol";

contract WithdrawalVerifierTest is DSTest {
    // Utilities
    Vm vm = Vm(HEVM_ADDRESS);
    bytes32 nonZeroHash = keccak256(abi.encode("NON_ZERO"));

    // Dependencies
    L2OutputOracle oracle;

    // Oracle constructor arguments
    address sequencer = 0x000000000000000000000000000000000000AbBa;
    uint256 submissionInterval = 1800;
    uint256 l2BlockTime = 2;
    bytes32 genesisL2Output = keccak256(abi.encode(0));
    uint256 historicalTotalBlocks = 100;

    // Test target
    WithdrawalVerifier wv;

    // Target constructor arguments
    address withdrawalsPredeploy = 0x4200000000000000000000000000000000000016; // check this value

    // Cache of the initial L2 timestamp
    uint256 startingBlockTimestamp;

    // By default the first block has timestamp zero, which will cause underflows in the tests
    uint256 initTime = 1000;


    constructor() {
        // Move time forward so we have a non-zero starting timestamp
        vm.warp(initTime);
        // Deploy the L2OutputOracle and transfer owernship to the sequencer
        oracle = new L2OutputOracle(
            submissionInterval,
            l2BlockTime,
            genesisL2Output,
            historicalTotalBlocks,
            sequencer
        );
        startingBlockTimestamp = block.timestamp;

        wv = new WithdrawalVerifier(
            oracle,
            withdrawalsPredeploy
        );
    }
}




    // function setUp() external {
    //     lb = new L1Block();
    //     depositor = lb.DEPOSITOR_ACCOUNT();
    //     vm.prank(depositor);
    //     lb.setL1BlockValues(1, 2, 3, NON_ZERO_HASH);
    // }

    // function test_number() external {
    //     assertEq(lb.number(), 1);
    // }

    // function test_timestamp() external {
    //     assertEq(lb.timestamp(), 2);
    // }

    // function test_basefee() external {
    //     assertEq(lb.basefee(), 3);
    // }

    // function test_hash() external {
    //     assertEq(lb.hash(), NON_ZERO_HASH);
    // }
// }
