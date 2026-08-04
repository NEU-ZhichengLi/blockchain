#!/usr/bin/env bash
set -euo pipefail

: "${CHANNEL_NAME:=vqchannel}"
: "${CHAINCODE_NAME:=vqstate}"
: "${CHAINCODE_VERSION:=1.0}"
: "${CHAINCODE_SEQUENCE:?set CHAINCODE_SEQUENCE}"

peer lifecycle chaincode commit \
  --channelID "${CHANNEL_NAME}" \
  --name "${CHAINCODE_NAME}" \
  --version "${CHAINCODE_VERSION}" \
  --sequence "${CHAINCODE_SEQUENCE}" \
  --signature-policy "AND('Org1MSP.peer','Org2MSP.peer')" \
  "$@"
