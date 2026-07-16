#!/usr/bin/env bash
set -euo pipefail

export PATH="/root/fabric-samples/bin:/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export FABRIC_CFG_PATH="${FABRIC_CFG_PATH:-/root/fabric-samples/config}"

TEST_NETWORK="${TEST_NETWORK:-$HOME/fabric-samples/test-network}"
CHANNEL_NAME="${CHANNEL_NAME:-vqchannel}"
CHAINCODE_NAME="${CHAINCODE_NAME:-vqfresh}"
BATCH_ID="${BATCH_ID:-window-0}"
EPOCH="${EPOCH:-1}"
THETA="${THETA:-theta-demo}"
SIGNATURE="${SIGNATURE:-sig-demo}"

cd "$TEST_NETWORK"

sudo service docker start >/dev/null 2>&1 || true
docker-compose -f compose/compose-test-net.yaml start >/dev/null

set +u
source scripts/envVar.sh
setGlobals 1
set -u

for attempt in 1 2 3 4 5; do
  if peer chaincode invoke \
    -o localhost:7050 \
    --ordererTLSHostnameOverride orderer.example.com \
    --tls \
    --cafile "$ORDERER_CA" \
    -C "$CHANNEL_NAME" \
    -n "$CHAINCODE_NAME" \
    --peerAddresses localhost:7051 \
    --tlsRootCertFiles "$PEER0_ORG1_CA" \
    --peerAddresses localhost:9051 \
    --tlsRootCertFiles "$PEER0_ORG2_CA" \
    --waitForEvent \
    -c "{\"Args\":[\"Publish\",\"$BATCH_ID\",\"$EPOCH\",\"$THETA\",\"$SIGNATURE\"]}"; then
    break
  fi
  if [ "$attempt" = "5" ]; then
    exit 1
  fi
  sleep 3
done

sleep 2

echo "Get($BATCH_ID):"
peer chaincode query \
  -C "$CHANNEL_NAME" \
  -n "$CHAINCODE_NAME" \
  -c "{\"Args\":[\"Get\",\"$BATCH_ID\"]}"

echo "Latest:"
peer chaincode query \
  -C "$CHANNEL_NAME" \
  -n "$CHAINCODE_NAME" \
  -c '{"Args":["Latest"]}'
