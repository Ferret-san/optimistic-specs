#!/bin/sh
set -exu

VERBOSITY=${ERIGON_VERBOSITY:-3}
ERIGON_DATA_DIR=/db
CHAIN_ID=$(cat /genesis.json | jq -r .config.chainId)
BLOCK_SIGNER_PRIVATE_KEY="3e4bde571b86929bf08e2aaad9a6a1882664cd5e65b96fff7d03e1c4e6dfa15c"
BLOCK_SIGNER_ADDRESS="0xca062b0fd91172d89bcd4bb084ac4e21972cc467"

echo -n "pwd" > "$ERIGON_DATA_DIR"/password
echo -n "$BLOCK_SIGNER_PRIVATE_KEY" | sed 's/0x//' > "$ERIGON_DATA_DIR"/block-signer-key
exec erigon  \
	--datadir="$ERIGON_DATA_DIR" \
	--verbosity="$VERBOSITY" \
	--chain dev \
	--http \
	--http.corsdomain="*" \
	--http.vhosts="*" \
	--http.addr=0.0.0.0 \
	--http.port=8545 \
	--http.api=web3,debug,eth,txpool,net,engine \
	--ws \
	--syncmode=full \
	--nodiscover \
	--maxpeers=1 \
	--networkid=$CHAIN_ID \
	#--unlock=$BLOCK_SIGNER_ADDRESS \
	--mine \
	--miner.etherbase=$BLOCK_SIGNER_ADDRESS \
	--password="$ERIGON_DATA_DIR"/password \
	--allow-insecure-unlock \
	--gcmode=archive \
	"$@"