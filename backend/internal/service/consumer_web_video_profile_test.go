package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConsumerWebVideoDynamicModelProfile(t *testing.T) {
	first := ConsumerWebModel{ID: "runtime-pro-a", Capacity: 2, CapacityField: 12, ModelNumber: 3, ProfilePresent: true}
	second := ConsumerWebModel{ID: "runtime-pro-b", Capacity: 1, CapacityField: 13, ModelNumber: 5, ProfilePresent: true}
	a, err := consumerWebVideoModelHeader(first, "SESSION-A")
	if err != nil {
		t.Fatal(err)
	}
	b, err := consumerWebVideoModelHeader(second, "SESSION-B")
	if err != nil || a == b {
		t.Fatal("runtime profile did not change model header")
	}
	for _, tc := range []struct {
		header  string
		model   ConsumerWebModel
		session string
	}{{a, first, "SESSION-A"}, {b, second, "SESSION-B"}} {
		var fields []any
		if err := json.Unmarshal([]byte(tc.header), &fields); err != nil {
			t.Fatal(err)
		}
		if len(fields) < 17 || fields[4] != tc.model.ID || fields[len(fields)-1] != tc.session || fields[len(fields)-3] != float64(tc.model.ModelNumber) {
			t.Fatal("runtime model header missing profile component")
		}
		inner := consumerWebVideoGenerateInner("test", "en", "RID", tc.model.ModelNumber, nil)
		if len(inner) != 81 || inner[79] != tc.model.ModelNumber {
			t.Fatal("inner model number mismatch")
		}
	}
	if _, err := consumerWebVideoModelHeader(ConsumerWebModel{ID: "missing-profile"}, "SESSION"); err == nil {
		t.Fatal("missing source profile allowed")
	}
}

func TestConsumerWebVideoCapacityFromRegistry(t *testing.T) {
	for _, tc := range []struct {
		tier, cap   float64
		want, field int
	}{{21, 0, 1, 13}, {22, 0, 2, 13}, {0, 115, 4, 12}, {16, 0, 3, 12}, {8, 0, 2, 12}, {0, 0, 1, 12}} {
		inner := make([]any, 18)
		inner[16] = []any{tc.tier}
		inner[17] = []any{tc.cap}
		capacity, field, ok := consumerWebModelCapacity(inner)
		if !ok || capacity != tc.want || field != tc.field {
			t.Fatalf("capacity=%d field=%d", capacity, field)
		}
	}
	entry := make([]any, 18)
	entry[0], entry[1], entry[11], entry[17] = "runtime-id", "Pro", "Pro", float64(4)
	model, ok := consumerWebRegistryModel(entry)
	if !ok || model.ModelNumber != 4 {
		t.Fatal("model number not parsed")
	}
	if !strings.Contains(consumerWebClientSessionID(), "-") {
		t.Fatal("client session ID not UUID shaped")
	}
}
