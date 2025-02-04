package node

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/ethdb/memorydb"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ethereum-optimism/optimistic-specs/opnode/l2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/trie"
)

func ComputeL2OutputRoot(l2OutputRootVersion l2.Bytes32, blockHash common.Hash, blockRoot common.Hash, storageRoot common.Hash) l2.Bytes32 {
	var buf bytes.Buffer
	buf.Write(l2OutputRootVersion[:])
	buf.Write(blockRoot.Bytes())
	buf.Write(storageRoot[:])
	buf.Write(blockHash.Bytes())
	return l2.Bytes32(crypto.Keccak256Hash(buf.Bytes()))
}

type AccountResult struct {
	AccountProof []hexutil.Bytes `json:"accountProof"`

	Address     common.Address `json:"address"`
	Balance     *hexutil.Big   `json:"balance"`
	CodeHash    common.Hash    `json:"codeHash"`
	Nonce       hexutil.Uint64 `json:"nonce"`
	StorageHash common.Hash    `json:"storageHash"`
	// storageProof field is ignored, we only need to proof the account contents,
	// we do not access any individual storage values.
}

// Verify an account proof from the getProof RPC. See https://eips.ethereum.org/EIPS/eip-1186
func (res *AccountResult) Verify(stateRoot common.Hash) error {
	accountClaimed := []interface{}{uint64(res.Nonce), (*big.Int)(res.Balance).Bytes(), res.StorageHash, res.CodeHash}
	accountClaimedValue, err := rlp.EncodeToBytes(accountClaimed)
	if err != nil {
		return fmt.Errorf("failed to encode account from retrieved values: %v", err)
	}

	// create a db with all trie nodes
	db := memorydb.New()
	for i, encodedNode := range res.AccountProof {
		nodeKey := crypto.Keccak256(encodedNode)
		if err := db.Put(nodeKey, encodedNode); err != nil {
			return fmt.Errorf("failed to load proof value %d into mem db: %v", i, err)
		}
	}

	key := crypto.Keccak256Hash(res.Address[:])
	trieDB := trie.NewDatabase(db)

	// wrap our DB of trie nodes with a Trie interface, and anchor it at the trusted state root
	proofTrie, err := trie.New(stateRoot, trieDB)
	if err != nil {
		return fmt.Errorf("failed to load db wrapper around kv store")
	}

	// now get the full value from the account proof, and check that it matches the JSON contents
	accountProofValue, err := proofTrie.TryGet(key[:])
	if err != nil {
		return fmt.Errorf("failed to retrieve account value: %v", err)
	}

	if !bytes.Equal(accountClaimedValue, accountProofValue) {
		return fmt.Errorf("L1 RPC is tricking us, account proof does not match provided deserialized values:\n"+
			"  claimed: %x\n"+
			"  proof:   %x", accountClaimedValue, accountProofValue)
	}
	return err
}
