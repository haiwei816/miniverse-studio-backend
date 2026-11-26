package service

import (
	"testing"
)

func TestEncodeDecodeBoardMember_TieBreakOrder(t *testing.T) {
	// Max mode: higher score sorts first; equal score -> earlier reached_at sorts first.
	mTop := encodeBoardMember("max", 20000, 20000, "fixture-20000")
	mTieEarly := encodeBoardMember("max", 10000, 0, "p-early")
	mTieLate := encodeBoardMember("max", 10000, 10000, "fixture-10000")
	if !(mTop < mTieEarly && mTieEarly < mTieLate) {
		t.Fatalf("unexpected max order: %q, %q, %q", mTop, mTieEarly, mTieLate)
	}
	r, ok := decodeBoardMember("max", mTieEarly)
	if !ok || r.Score != 10000 || r.Reached != 0 || r.Player != "p-early" {
		t.Fatalf("decode failed: ok=%v rank=%+v", ok, r)
	}

	// Min mode: smaller score sorts first.
	mMin1 := encodeBoardMember("min", 10, 500, "p1")
	mMin2 := encodeBoardMember("min", 20, 100, "p2")
	if !(mMin1 < mMin2) {
		t.Fatalf("unexpected min order: %q vs %q", mMin1, mMin2)
	}
	rMin, ok := decodeBoardMember("min", mMin1)
	if !ok || rMin.Score != 10 || rMin.Reached != 500 || rMin.Player != "p1" {
		t.Fatalf("min decode failed: ok=%v rank=%+v", ok, rMin)
	}
}
