package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/hyperledger/fabric-contract-api-go/contractapi"
	"vqstream/stateboard/policy"
)

type StateBoardContract struct {
	contractapi.Contract
}

type PublicState struct {
	StreamID         string `json:"stream_id"`
	Window           uint64 `json:"window"`
	Root             string `json:"root"`
	PrefixCommitment string `json:"prefix_commitment"`
	Signature        string `json:"signature"`
}

func stateKey(streamID string, window uint64) string {
	return fmt.Sprintf("vqstream:state:%s:%020d", streamID, window)
}

func latestKey(streamID string) string {
	return "vqstream:latest:" + streamID
}

func ownerKey(streamID string) string {
	return "vqstream:owner:" + streamID
}

func validatePayload(root, prefixCommitment, signature string) error {
	for name, item := range map[string]struct {
		text string
		size int
	}{
		"root":              {root, 32},
		"prefix commitment": {prefixCommitment, 48},
		"signature":         {signature, 48},
	} {
		raw, err := base64.StdEncoding.DecodeString(item.text)
		if err != nil || len(raw) != item.size {
			return fmt.Errorf("%s must be canonical base64 for %d bytes", name, item.size)
		}
	}
	return nil
}

func parseWindow(text string) (uint64, error) {
	window, err := strconv.ParseUint(text, 10, 64)
	if err != nil || window == 0 {
		return 0, fmt.Errorf("window must be a positive integer")
	}
	return window, nil
}

func encodeState(state PublicState) ([]byte, error) {
	return json.Marshal(state)
}

func authorizePublisher(ctx contractapi.TransactionContextInterface, streamID string) error {
	mspID, err := ctx.GetClientIdentity().GetMSPID()
	if err != nil {
		return fmt.Errorf("read publisher MSP: %w", err)
	}
	certificate, err := ctx.GetClientIdentity().GetX509Certificate()
	if err != nil {
		return fmt.Errorf("read publisher certificate: %w", err)
	}
	if certificate == nil {
		return fmt.Errorf("publisher must use an X.509 identity")
	}
	if err := policy.ValidatePublisher(mspID, certificate.Subject.CommonName); err != nil {
		return err
	}
	publisherID, err := ctx.GetClientIdentity().GetID()
	if err != nil {
		return fmt.Errorf("read publisher identity: %w", err)
	}
	key := ownerKey(streamID)
	registeredID, err := ctx.GetStub().GetState(key)
	if err != nil {
		return err
	}
	if registeredID == nil {
		return ctx.GetStub().PutState(key, []byte(publisherID))
	}
	if string(registeredID) != publisherID {
		return fmt.Errorf("publisher is not the registered owner of stream %q", streamID)
	}
	return nil
}

func validateNextWindow(latestBytes []byte, window uint64) error {
	if latestBytes == nil {
		return policy.ValidateNextWindow(false, 0, window)
	}
	var latest PublicState
	if err := json.Unmarshal(latestBytes, &latest); err != nil {
		return err
	}
	return policy.ValidateNextWindow(true, latest.Window, window)
}

func (c *StateBoardContract) Publish(
	ctx contractapi.TransactionContextInterface,
	streamID, windowText, root, prefixCommitment, signature string,
) error {
	if streamID == "" {
		return fmt.Errorf("streamID is required")
	}
	window, err := parseWindow(windowText)
	if err != nil {
		return err
	}
	if err := validatePayload(root, prefixCommitment, signature); err != nil {
		return err
	}
	if err := authorizePublisher(ctx, streamID); err != nil {
		return err
	}

	latestBytes, err := ctx.GetStub().GetState(latestKey(streamID))
	if err != nil {
		return err
	}
	if err := validateNextWindow(latestBytes, window); err != nil {
		return err
	}

	state := PublicState{streamID, window, root, prefixCommitment, signature}
	stateBytes, err := encodeState(state)
	if err != nil {
		return err
	}
	historyKey := stateKey(streamID, window)
	existing, err := ctx.GetStub().GetState(historyKey)
	if err != nil {
		return err
	}
	if existing != nil {
		return fmt.Errorf("state %s/%d already exists", streamID, window)
	}
	if err := ctx.GetStub().PutState(historyKey, stateBytes); err != nil {
		return err
	}
	return ctx.GetStub().PutState(latestKey(streamID), stateBytes)
}

func (c *StateBoardContract) Get(
	ctx contractapi.TransactionContextInterface, streamID, windowText string,
) (*PublicState, error) {
	window, err := parseWindow(windowText)
	if err != nil {
		return nil, err
	}
	stateBytes, err := ctx.GetStub().GetState(stateKey(streamID, window))
	if err != nil {
		return nil, err
	}
	return decodeState(stateBytes, fmt.Sprintf("state %s/%d", streamID, window))
}

func (c *StateBoardContract) Latest(
	ctx contractapi.TransactionContextInterface, streamID string,
) (*PublicState, error) {
	stateBytes, err := ctx.GetStub().GetState(latestKey(streamID))
	if err != nil {
		return nil, err
	}
	return decodeState(stateBytes, "latest state for "+streamID)
}

func decodeState(raw []byte, label string) (*PublicState, error) {
	if raw == nil {
		return nil, fmt.Errorf("%s not found", label)
	}
	var state PublicState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func main() {
	chaincode, err := contractapi.NewChaincode(&StateBoardContract{})
	if err != nil {
		panic(err)
	}
	if err := chaincode.Start(); err != nil {
		panic(err)
	}
}
