package main

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/hyperledger/fabric-contract-api-go/contractapi"
)

type FreshnessContract struct {
	contractapi.Contract
}

type FreshnessState struct {
	BatchID   string `json:"batch_id"`
	Epoch     int    `json:"epoch"`
	Theta     string `json:"theta"`
	Signature string `json:"signature"`
}

const latestKey = "vqstream:latest"

func stateKey(batchID string) string {
	return "vqstream:state:" + batchID
}

func (c *FreshnessContract) Publish(ctx contractapi.TransactionContextInterface, batchID string, epochText string, theta string, signature string) error {
	if batchID == "" {
		return fmt.Errorf("batchID is required")
	}
	epoch, err := strconv.Atoi(epochText)
	if err != nil {
		return fmt.Errorf("invalid epoch: %w", err)
	}

	key := stateKey(batchID)
	existingBytes, err := ctx.GetStub().GetState(key)
	if err != nil {
		return err
	}
	if existingBytes != nil {
		var existing FreshnessState
		if err := json.Unmarshal(existingBytes, &existing); err != nil {
			return err
		}
		if epoch < existing.Epoch {
			return fmt.Errorf("stale epoch %d for batch %s; current epoch is %d", epoch, batchID, existing.Epoch)
		}
	}

	state := FreshnessState{
		BatchID:   batchID,
		Epoch:     epoch,
		Theta:     theta,
		Signature: signature,
	}
	stateBytes, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := ctx.GetStub().PutState(key, stateBytes); err != nil {
		return err
	}
	return ctx.GetStub().PutState(latestKey, []byte(batchID))
}

func (c *FreshnessContract) Get(ctx contractapi.TransactionContextInterface, batchID string) (*FreshnessState, error) {
	stateBytes, err := ctx.GetStub().GetState(stateKey(batchID))
	if err != nil {
		return nil, err
	}
	if stateBytes == nil {
		return nil, fmt.Errorf("freshness state not found for batch %s", batchID)
	}
	var state FreshnessState
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (c *FreshnessContract) Latest(ctx contractapi.TransactionContextInterface) (*FreshnessState, error) {
	latestBytes, err := ctx.GetStub().GetState(latestKey)
	if err != nil {
		return nil, err
	}
	if latestBytes == nil {
		return nil, fmt.Errorf("no latest VQStream freshness state")
	}
	return c.Get(ctx, string(latestBytes))
}

func main() {
	chaincode, err := contractapi.NewChaincode(&FreshnessContract{})
	if err != nil {
		panic(err)
	}
	if err := chaincode.Start(); err != nil {
		panic(err)
	}
}
