package l1

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum-optimism/optimistic-specs/opnode/rollup"

	"github.com/ethereum-optimism/optimistic-specs/opnode/eth"
	"github.com/ethereum-optimism/optimistic-specs/opnode/rollup/derive"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
	lru "github.com/hashicorp/golang-lru"
)

type SourceConfig struct {
	MaxParallelBatching int
	MaxBatchRetry       int
	MaxRequestsPerBatch int

	// cache sizes

	// Number of blocks worth of receipts to cache
	ReceiptsCacheSize int
	// Number of blocks worth of transactions to cache
	TransactionsCacheSize int
	// Number of
	HeadersCacheSize int

	// If the RPC is untrusted, then we should not use cached information from responses,
	// and instead verify against the block-hash.
	// Of real L1 blocks no deposits can be missed/faked, no batches can be missed/faked,
	// only the wrong L1 blocks can be retrieved.
	TrustRPC bool
}

func DefaultConfig(config *rollup.Config, trustRPC bool) SourceConfig {
	return SourceConfig{
		// We only consume receipts once per block,
		// we just need basic redundancy if we share the cache between multiple drivers
		ReceiptsCacheSize: 20,

		// Optimal if at least a few times the size of a sequencing window.
		// When smaller than a window, requests would be repeated every window shift.
		// Additional cache-size for handling reorgs, and thus more unique blocks, also helps.
		TransactionsCacheSize: int(config.SeqWindowSize * 4),
		HeadersCacheSize:      int(config.SeqWindowSize * 4),

		// TODO: tune batch params
		MaxParallelBatching: 8,
		MaxBatchRetry:       3,
		MaxRequestsPerBatch: 20,

		TrustRPC: trustRPC,
	}
}

type batchCallContextFn func(ctx context.Context, b []rpc.BatchElem) error

type callContextFn func(ctx context.Context, result interface{}, method string, args ...interface{}) error

// Source to retrieve L1 data from with optimized batch requests, cached results,
// and flag to not trust the RPC.
type Source struct {
	client *ethclient.Client

	batchCall batchCallContextFn
	call      callContextFn

	trustRPC bool

	// cache receipts in bundles per block hash
	// common.Hash -> types.Receipts
	receiptsCache *lru.Cache

	// cache transactions in bundles per block hash
	// common.Hash -> types.Transactions
	transactionsCache *lru.Cache

	// cache block headers of blocks by hash
	// common.Hash -> *HeaderInfo
	headersCache *lru.Cache
}

func NewSource(client *rpc.Client, log log.Logger, config SourceConfig) *Source {
	receiptsCache, _ := lru.New(config.ReceiptsCacheSize)
	transactionsCache, _ := lru.New(config.TransactionsCacheSize)
	headersCache, _ := lru.New(config.HeadersCacheSize)

	// Batch calls will be split up to handle max-batch size,
	// and parallelized since the RPC server does not parallelize batch contents otherwise.
	getBatch := parallelBatchCall(log, client.BatchCallContext,
		config.MaxBatchRetry, config.MaxRequestsPerBatch, config.MaxParallelBatching)

	return &Source{
		client:            ethclient.NewClient(client),
		batchCall:         getBatch,
		call:              client.CallContext,
		trustRPC:          config.TrustRPC,
		receiptsCache:     receiptsCache,
		transactionsCache: transactionsCache,
		headersCache:      headersCache,
	}
}

// SubscribeNewHead subscribes to notifications about the current blockchain head on the given channel.
func (s *Source) SubscribeNewHead(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error) {
	// Note that *types.Header does not cache the block hash unlike *HeaderInfo, it always recomputes.
	// Inefficient if used poorly, but no trust issue.
	return s.client.SubscribeNewHead(ctx, ch)
}

func (s *Source) headerCall(ctx context.Context, method string, id interface{}) (*HeaderInfo, error) {
	var header *rpcHeader
	err := s.call(ctx, &header, method, id, false) // headers are just blocks without txs
	if err != nil {
		return nil, err
	}
	if header == nil {
		return nil, ethereum.NotFound
	}
	info, err := header.Info(s.trustRPC)
	if err != nil {
		return nil, err
	}
	s.headersCache.Add(info.hash, info)
	return info, nil
}

func (s *Source) blockCall(ctx context.Context, method string, id interface{}) (*HeaderInfo, types.Transactions, error) {
	var block *rpcBlock
	err := s.call(ctx, &block, method, id, true)
	if err != nil {
		return nil, nil, err
	}
	if block == nil {
		return nil, nil, ethereum.NotFound
	}
	info, txs, err := block.Info(s.trustRPC)
	if err != nil {
		return nil, nil, err
	}
	s.headersCache.Add(info.hash, info)
	s.transactionsCache.Add(info.hash, txs)
	return info, txs, nil
}

func (s *Source) InfoByHash(ctx context.Context, hash common.Hash) (derive.L1Info, error) {
	if header, ok := s.headersCache.Get(hash); ok {
		return header.(*HeaderInfo), nil
	}
	return s.headerCall(ctx, "eth_getBlockByHash", hash)
}

func (s *Source) InfoByNumber(ctx context.Context, number uint64) (derive.L1Info, error) {
	// can't hit the cache when querying by number due to reorgs.
	return s.headerCall(ctx, "eth_getBlockByNumber", hexutil.EncodeUint64(number))
}

func (s *Source) InfoHead(ctx context.Context) (derive.L1Info, error) {
	// can't hit the cache when querying the head due to reorgs / changes.
	return s.headerCall(ctx, "eth_getBlockByNumber", "latest")
}

func (s *Source) InfoAndTxsByHash(ctx context.Context, hash common.Hash) (derive.L1Info, types.Transactions, error) {
	if header, ok := s.headersCache.Get(hash); ok {
		if txs, ok := s.transactionsCache.Get(hash); ok {
			return header.(*HeaderInfo), txs.(types.Transactions), nil
		}
	}
	return s.blockCall(ctx, "eth_getBlockByHash", hash)
}

func (s *Source) InfoAndTxsByNumber(ctx context.Context, number uint64) (derive.L1Info, types.Transactions, error) {
	// can't hit the cache when querying by number due to reorgs.
	return s.blockCall(ctx, "eth_getBlockByNumber", hexutil.EncodeUint64(number))
}

func (s *Source) InfoAndTxsHead(ctx context.Context) (derive.L1Info, types.Transactions, error) {
	// can't hit the cache when querying the head due to reorgs / changes.
	return s.blockCall(ctx, "eth_getBlockByNumber", "latest")
}

func (s *Source) Fetch(ctx context.Context, blockHash common.Hash) (derive.L1Info, types.Transactions, types.Receipts, error) {
	if blockHash == (common.Hash{}) {
		return nil, nil, nil, ethereum.NotFound
	}
	info, txs, err := s.blockCall(ctx, "eth_getBlockByHash", blockHash)
	if err != nil {
		return nil, nil, nil, err
	}

	receipts, err := fetchReceipts(ctx, info.receiptHash, txs, s.batchCall)
	if err != nil {
		return nil, nil, nil, err
	}
	s.receiptsCache.Add(info.hash, receipts)
	return info, txs, receipts, nil
}

// FetchAllTransactions fetches transaction lists of a window of blocks, and caches each block and the transactions
func (s *Source) FetchAllTransactions(ctx context.Context, window []eth.BlockID) ([]types.Transactions, error) {
	// list of transaction lists
	allTxLists := make([]types.Transactions, len(window))

	var blockRequests []rpc.BatchElem
	var requestIndices []int

	for i := 0; i < len(window); i++ {
		// if we are shifting the window by 1 block at a time, most of the results should already be in the cache.
		txs, ok := s.transactionsCache.Get(window[i].Hash)
		if ok {
			allTxLists[i] = txs.(types.Transactions)
		} else {
			blockRequests = append(blockRequests, rpc.BatchElem{
				Method: "eth_getBlockByHash",
				Args:   []interface{}{window[i].Hash, true}, // request block including transactions list
				Result: new(rpcBlock),
				Error:  nil,
			})
			requestIndices = append(requestIndices, i) // remember the block index this request corresponds to
		}
	}

	if err := s.batchCall(ctx, blockRequests); err != nil {
		return nil, err
	}

	// try to cache everything we have before halting on the results with errors
	for i := 0; i < len(blockRequests); i++ {
		if blockRequests[i].Error == nil {
			info, txs, err := blockRequests[i].Result.(*rpcBlock).Info(s.trustRPC)
			if err != nil {
				return nil, fmt.Errorf("bad block data for block %s: %v", blockRequests[i].Args[0], err)
			}
			s.headersCache.Add(info.hash, info)
			s.transactionsCache.Add(info.hash, txs)
			allTxLists[requestIndices[i]] = txs
		}
	}

	for i := 0; i < len(blockRequests); i++ {
		if blockRequests[i].Error != nil {
			return nil, fmt.Errorf("failed to retrieve transactions of block %s in batch of %d blocks: %v", window[i], len(blockRequests), blockRequests[i].Error)
		}
	}

	return allTxLists, nil
}

func (s *Source) refFromStart(ctx context.Context, info derive.L1Info) (eth.L1BlockRef, error) {
	if info.NumberU64() == 0 {
		return eth.L1BlockRef{Self: info.ID()}, nil // L1 genesis block has zeroed parent hash / number
	}
	parent, err := s.InfoByHash(ctx, info.ParentHash())
	if err != nil {
		return eth.L1BlockRef{}, fmt.Errorf("failed to fetch parent of %s: %v", info.ID(), err)
	}
	return eth.L1BlockRef{Self: info.ID(), Parent: parent.ID()}, nil
}

func (s *Source) L1HeadBlockRef(ctx context.Context) (eth.L1BlockRef, error) {
	head, err := s.InfoHead(ctx)
	if err != nil {
		return eth.L1BlockRef{}, fmt.Errorf("failed to fetch head header: %v", err)
	}
	return s.refFromStart(ctx, head)
}

func (s *Source) L1BlockRefByNumber(ctx context.Context, l1Num uint64) (eth.L1BlockRef, error) {
	head, err := s.InfoByNumber(ctx, l1Num)
	if err != nil {
		return eth.L1BlockRef{}, fmt.Errorf("failed to fetch header by num %d: %v", l1Num, err)
	}
	return s.refFromStart(ctx, head)
}

// L1Range returns a range of L1 block beginning just after begin, up to max blocks.
// This batch-requests all blocks by number in the range at once, and then verifies the consistency
func (s *Source) L1Range(ctx context.Context, begin eth.BlockID, max uint64) ([]eth.BlockID, error) {

	headerRequests := make([]rpc.BatchElem, max)
	for i := uint64(0); i < max; i++ {
		headerRequests[i] = rpc.BatchElem{
			Method: "eth_getBlockByNumber",
			Args:   []interface{}{hexutil.EncodeUint64(begin.Number + 1 + i), false},
			Result: new(*rpcHeader),
			Error:  nil,
		}
	}
	if err := s.batchCall(ctx, headerRequests); err != nil {
		return nil, err
	}

	out := make([]eth.BlockID, 0, max)

	// try to cache everything we have before halting on the results with errors
	for i := 0; i < len(headerRequests); i++ {
		result := *headerRequests[i].Result.(**rpcHeader)
		if headerRequests[i].Error == nil {
			if result == nil {
				break // no more headers from here
			}
			info, err := result.Info(s.trustRPC)
			if err != nil {
				return nil, fmt.Errorf("bad header data for block %s: %v", headerRequests[i].Args[0], err)
			}
			s.headersCache.Add(info.hash, info)
			out = append(out, info.ID())
			prev := begin
			if i > 0 {
				prev = out[i-1]
			}
			if prev.Hash != info.parentHash {
				return nil, fmt.Errorf("inconsistent results from L1 chain range request, block %s not expected parent %s of %s", prev, info.parentHash, info.ID())
			}
		} else if errors.Is(headerRequests[i].Error, ethereum.NotFound) {
			break // no more headers from here
		} else {
			return nil, fmt.Errorf("failed to retrieve block: %s: %v", headerRequests[i].Args[0], headerRequests[i].Error)
		}
	}
	return out, nil
}

func (s *Source) Close() {
	s.client.Close()
}
