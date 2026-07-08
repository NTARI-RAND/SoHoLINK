package operator

import "testing"

// zeroWindow returns a fresh 256-bit (32-byte) all-zero window.
func zeroWindow() []byte { return make([]byte, seqWindowBits/8) }

func TestAdvanceWindow_FirstTransmissionAccepted(t *testing.T) {
	// Virgin window: seq_high=0, all-zero bitmap. Any first seq is accepted.
	high, w, ok := advanceWindow(0, zeroWindow(), 5)
	if !ok {
		t.Fatal("first transmission should be accepted")
	}
	if high != 5 {
		t.Errorf("high = %d, want 5", high)
	}
	if !getBit(w, 0) {
		t.Error("new high's own bit should be set")
	}
}

func TestAdvanceWindow_MonotonicAdvance(t *testing.T) {
	high, w := uint64(0), zeroWindow()
	var ok bool
	for _, seq := range []uint64{1, 2, 3, 10, 11} {
		high, w, ok = advanceWindow(high, w, seq)
		if !ok {
			t.Fatalf("seq %d should advance", seq)
		}
		if high != seq {
			t.Fatalf("after seq %d, high=%d", seq, high)
		}
	}
}

func TestAdvanceWindow_ExactReplayOfHighRejected(t *testing.T) {
	high, w, ok := advanceWindow(0, zeroWindow(), 7)
	if !ok {
		t.Fatal("setup: first accept")
	}
	// Re-sending the same seq (now the high watermark) is a replay.
	if _, _, ok := advanceWindow(high, w, 7); ok {
		t.Error("replay of the high watermark should be rejected")
	}
}

func TestAdvanceWindow_InWindowReplayRejected(t *testing.T) {
	// Accept 5, then 8. 5 and 6,7 are inside the window below 8.
	high, w, _ := advanceWindow(0, zeroWindow(), 5)
	high, w, _ = advanceWindow(high, w, 8)

	// 5 was seen (it was the previous high, recorded on the shift) -> replay.
	if _, _, ok := advanceWindow(high, w, 5); ok {
		t.Error("in-window replay of a seen seq (5) should be rejected")
	}
	// 6 was never seen -> accept once.
	high2, w2, ok := advanceWindow(high, w, 6)
	if !ok {
		t.Fatal("in-window fresh seq (6) should be accepted")
	}
	if high2 != high {
		t.Errorf("accepting an in-window seq must not change high (got %d, want %d)", high2, high)
	}
	// 6 again -> now a replay.
	if _, _, ok := advanceWindow(high2, w2, 6); ok {
		t.Error("second submission of 6 should be rejected")
	}
}

func TestAdvanceWindow_BelowWindowRejected(t *testing.T) {
	// Advance the high well past the window width, then try an ancient seq.
	high, w, _ := advanceWindow(0, zeroWindow(), 1000)
	old := high - seqWindowBits // exactly at the low edge -> too old
	if _, _, ok := advanceWindow(high, w, old); ok {
		t.Error("seq at/below the window low edge should be rejected")
	}
	if _, _, ok := advanceWindow(high, w, high-seqWindowBits-1); ok {
		t.Error("seq below the window should be rejected")
	}
}

func TestAdvanceWindow_LargeJumpClearsWindow(t *testing.T) {
	// A jump larger than the window width resets the bitmap to only the new high.
	high, w, _ := advanceWindow(0, zeroWindow(), 5)
	high, w, ok := advanceWindow(high, w, 5+seqWindowBits+100)
	if !ok {
		t.Fatal("large forward jump should be accepted")
	}
	if !getBit(w, 0) {
		t.Error("new high bit should be set after a clearing jump")
	}
	// The old high (5) is now far below the window: rejected.
	if _, _, ok := advanceWindow(high, w, 5); ok {
		t.Error("pre-jump seq should be below the window after a large jump")
	}
}

func TestAdvanceWindow_MalformedWindowFailsClosed(t *testing.T) {
	if _, _, ok := advanceWindow(0, []byte{0x00}, 1); ok {
		t.Error("a wrong-width window must fail closed")
	}
}

func TestBitOps(t *testing.T) {
	b := zeroWindow()
	setBit(b, 0)
	if !getBit(b, 0) || b[0] != 0x80 {
		t.Errorf("bit 0 should be MSB of byte 0, got %#x", b[0])
	}
	setBit(b, 8)
	if !getBit(b, 8) || b[1] != 0x80 {
		t.Errorf("bit 8 should be MSB of byte 1, got %#x", b[1])
	}
	setBit(b, 255)
	if !getBit(b, 255) || b[31] != 0x01 {
		t.Errorf("bit 255 should be LSB of byte 31, got %#x", b[31])
	}
}
