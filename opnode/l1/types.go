package l1

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ethereum-optimism/optimistic-specs/opnode/eth"
	"github.com/ethereum-optimism/optimistic-specs/opnode/rollup/derive"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/trie"
)

// Note: we do this ugly typing because we want the best, and the standard bindings are not sufficient:
// - batched calls of many block requests (standard bindings do extra uncle-header fetches, cannot be batched nicely)
// - ignore uncle data (does not even exist anymore post-Merge)
// - use cached transaction-sender data, if we trust the RPC.
// - use cached block hash, if we trust the RPC.
// - verify transactions list matches tx-root, to ensure consistency with block-hash, if we do not trust the RPC
//
// This way we minimize RPC calls, enable batching, and can choose to verify what the RPC gives us.

type HeaderInfo struct {
	hash        common.Hash
	parentHash  common.Hash
	root        common.Hash
	number      uint64
	time        uint64
	mixDigest   common.Hash // a.k.a. the randomness field post-merge.
	baseFee     *big.Int
	txHash      common.Hash
	receiptHash common.Hash
}

var _ derive.L1Info = (*HeaderInfo)(nil)

func (info *HeaderInfo) Hash() common.Hash {
	return info.hash
}

func (info *HeaderInfo) ParentHash() common.Hash {
	return info.parentHash
}

func (info *HeaderInfo) Root() common.Hash {
	return info.root
}

func (info *HeaderInfo) NumberU64() uint64 {
	return info.number
}

func (info *HeaderInfo) Time() uint64 {
	return info.time
}

func (info *HeaderInfo) MixDigest() common.Hash {
	return info.mixDigest
}

func (info *HeaderInfo) BaseFee() *big.Int {
	return info.baseFee
}

func (info *HeaderInfo) ID() eth.BlockID {
	return eth.BlockID{Hash: info.hash, Number: info.number}
}

type rpcHeaderCacheInfo struct {
	Hash common.Hash `json:"hash"`
}

type rpcHeader struct {
	cache  rpcHeaderCacheInfo
	header types.Header
}

func (header *rpcHeader) UnmarshalJSON(msg []byte) error {
	if err := json.Unmarshal(msg, &header.header); err != nil {
		return err
	}
	return json.Unmarshal(msg, &header.cache)
}

func (header *rpcHeader) Info(trustCache bool) (*HeaderInfo, error) {
	info := HeaderInfo{
		hash:        header.cache.Hash,
		parentHash:  header.header.ParentHash,
		root:        header.header.Root,
		number:      header.header.Number.Uint64(),
		time:        header.header.Time,
		mixDigest:   header.header.MixDigest,
		baseFee:     header.header.BaseFee,
		txHash:      header.header.TxHash,
		receiptHash: header.header.ReceiptHash,
	}
	if !trustCache {
		if computed := header.header.Hash(); computed != info.hash {
			return nil, fmt.Errorf("failed to verify block hash: computed %s but RPC said %s", computed, info.hash)
		}
	}
	return &info, nil
}

type rpcBlockCacheInfo struct {
	Transactions []rpcTransaction `json:"transactions"`
}

type rpcBlock struct {
	header rpcHeader
	cache  rpcBlockCacheInfo
}

func (block *rpcBlock) UnmarshalJSON(msg []byte) error {
	if err := json.Unmarshal(msg, &block.header); err != nil {
		return err
	}
	return json.Unmarshal(msg, &block.cache)
}

func (block *rpcBlock) Info(trustCache bool) (*HeaderInfo, types.Transactions, error) {
	// verify the header data
	info, err := block.header.Info(trustCache)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to verify block from RPC: %v", err)
	}

	txs := make([]*types.Transaction, len(block.cache.Transactions))
	for i := 0; i < len(block.cache.Transactions); i++ {
		tx := block.cache.Transactions[i]
		if trustCache && tx.cache.From != nil { // cache the sender (lazily compute it later if we don't trust the RPC)
			ethclient.SetSenderFromServer(tx.tx, *tx.cache.From, info.hash)
		}
		txs[i] = tx.tx
	}
	if !trustCache { // verify the list of transactions matches the tx-root
		hasher := trie.NewStackTrie(nil)
		computed := types.DeriveSha(types.Transactions(txs), hasher)
		if expected := info.txHash; expected != computed {
			return nil, nil, fmt.Errorf("failed to verify transactions list: expected transactions root %s but retrieved %s", expected, computed)
		}
	}
	return info, txs, nil
}

type rpcTransactionCacheInfo struct {
	// just ignore blockNumber and blockHash extra data
	From *common.Address `json:"from,omitempty"`
}

type rpcTransaction struct {
	tx    *types.Transaction
	cache rpcTransactionCacheInfo
}

func (tx *rpcTransaction) UnmarshalJSON(msg []byte) error {
	if err := json.Unmarshal(msg, &tx.tx); err != nil {
		return err
	}
	return json.Unmarshal(msg, &tx.cache)
}
