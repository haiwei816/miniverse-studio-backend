package service

import (
	"testing"
	"time"
)

func TestActivityValidationAndDailyBucket(t *testing.T) {
	a := Activity{ID: "test", Version: 1, Kind: "daily", Event: "play", Target: 2, Starts: 1, Ends: 3, Item: "wood", Amount: 1}
	if e := validateActivity(a); e != nil {
		t.Fatal(e)
	}
	zero := a
	zero.Amount = 0
	if validateActivity(zero) == nil {
		t.Fatal("accepted zero reward")
	}
	for _, kind := range []string{"unknown", "", "script"} {
		b := a
		b.Kind = kind
		if validateActivity(b) == nil {
			t.Fatal("accepted unknown kind")
		}
	}
	x := time.Date(2026, 11, 6, 23, 59, 59, 0, time.UTC)
	if bucketFor(a, x) == bucketFor(a, x.Add(time.Second)) {
		t.Fatal("daily bucket did not roll")
	}
	a.Kind = "cumulative"
	if bucketFor(a, x) != bucketFor(a, x.Add(24*time.Hour)) {
		t.Fatal("cumulative bucket rolled")
	}
}
