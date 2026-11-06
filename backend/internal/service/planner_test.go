package service

import (
	"encoding/json"
	"testing"
)

func TestPlannerSchemaAndSimulation(t *testing.T) {
	a := Activity{ID: "test", Version: 1, Kind: "daily", Event: "visit", Target: 2, Starts: 1, Ends: 200000, ClientAllowed: true, Item: "wood", Amount: 5}
	raw, _ := json.Marshal(a)
	if e := schemaCheck(raw); e != nil {
		t.Fatal(e)
	}
	if schemaCheck([]byte(`{"id":"unsafe","script":"execute"}`)) == nil {
		t.Fatal("untrusted config accepted")
	}
	v := simulate(a, []SimulationEvent{{1, 2}, {1, 3}, {1, 86402}, {1, 86403}, {99, 300000}})
	if v["reward_count"] != 2 || v["reward_total"] != int64(10) {
		t.Fatal(v)
	}
}
