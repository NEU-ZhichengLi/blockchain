package main

import (
	"encoding/base64"
	"reflect"
	"testing"
)

func encoded(size int) string {
	return base64.StdEncoding.EncodeToString(make([]byte, size))
}

func TestProductionContractDoesNotExposePopulate(t *testing.T) {
	if _, found := reflect.TypeOf(&StateBoardContract{}).MethodByName("Populate"); found {
		t.Fatal("production contract exposes Populate")
	}
}

func TestValidateNextWindowRequiresContiguousHistory(t *testing.T) {
	if err := validateNextWindow(nil, 1); err != nil {
		t.Fatalf("first window rejected: %v", err)
	}
	if err := validateNextWindow(nil, 2); err == nil {
		t.Fatal("non-one first window accepted")
	}
	latest, err := encodeState(PublicState{StreamID: "stream", Window: 7})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateNextWindow(latest, 8); err != nil {
		t.Fatalf("contiguous window rejected: %v", err)
	}
	for _, window := range []uint64{6, 7, 9} {
		if err := validateNextWindow(latest, window); err == nil {
			t.Fatalf("non-contiguous window %d accepted", window)
		}
	}
}

func TestPayloadRequiresCanonicalFieldSizes(t *testing.T) {
	if err := validatePayload(encoded(32), encoded(48), encoded(48)); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}
	if err := validatePayload(encoded(31), encoded(48), encoded(48)); err == nil {
		t.Fatal("short root accepted")
	}
	if err := validatePayload(encoded(32), encoded(47), encoded(48)); err == nil {
		t.Fatal("short prefix commitment accepted")
	}
	if err := validatePayload(encoded(32), encoded(48), encoded(47)); err == nil {
		t.Fatal("short signature accepted")
	}
}
