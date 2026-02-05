package merkle

import (
	"encoding/hex"
	"testing"
)

func TestNewTreeSingleLeaf(t *testing.T) {
	leaves := [][]byte{[]byte("hello")}
	tree, err := NewTree(leaves)
	if err != nil {
		t.Fatalf("NewTree failed: %v", err)
	}
	if tree.LeafCount() != 1 {
		t.Errorf("expected 1 leaf, got %d", tree.LeafCount())
	}
	if tree.Height() != 0 {
		t.Errorf("single leaf tree should have height 0, got %d", tree.Height())
	}
	if len(tree.Root()) != 32 {
		t.Errorf("root hash should be 32 bytes (SHA3-256), got %d", len(tree.Root()))
	}
}

func TestNewTreeTwoLeaves(t *testing.T) {
	leaves := [][]byte{[]byte("hello"), []byte("world")}
	tree, err := NewTree(leaves)
	if err != nil {
		t.Fatalf("NewTree failed: %v", err)
	}
	if tree.LeafCount() != 2 {
		t.Errorf("expected 2 leaves, got %d", tree.LeafCount())
	}
	if tree.Height() != 1 {
		t.Errorf("two-leaf tree should have height 1, got %d", tree.Height())
	}
}

func TestNewTreePowerOfTwo(t *testing.T) {
	leaves := make([][]byte, 8)
	for i := range leaves {
		leaves[i] = []byte{byte(i)}
	}

	tree, err := NewTree(leaves)
	if err != nil {
		t.Fatalf("NewTree failed: %v", err)
	}
	if tree.LeafCount() != 8 {
		t.Errorf("expected 8 leaves, got %d", tree.LeafCount())
	}
	if tree.Height() != 3 {
		t.Errorf("8-leaf tree should have height 3, got %d", tree.Height())
	}
}

func TestNewTreeOddLeaves(t *testing.T) {
	leaves := make([][]byte, 5)
	for i := range leaves {
		leaves[i] = []byte{byte(i)}
	}

	tree, err := NewTree(leaves)
	if err != nil {
		t.Fatalf("NewTree failed: %v", err)
	}
	if tree.LeafCount() != 5 {
		t.Errorf("expected 5 leaves, got %d", tree.LeafCount())
	}
}

func TestNewTreeEmpty(t *testing.T) {
	_, err := NewTree([][]byte{})
	if err == nil {
		t.Error("expected error for empty leaves")
	}
}

func TestTreeDeterministic(t *testing.T) {
	leaves := [][]byte{[]byte("a"), []byte("b"), []byte("c")}

	tree1, _ := NewTree(leaves)
	tree2, _ := NewTree(leaves)

	root1 := hex.EncodeToString(tree1.Root())
	root2 := hex.EncodeToString(tree2.Root())

	if root1 != root2 {
		t.Errorf("trees should be deterministic: %s != %s", root1, root2)
	}
}

func TestProofAndVerify(t *testing.T) {
	leaves := [][]byte{
		[]byte("event1"),
		[]byte("event2"),
		[]byte("event3"),
		[]byte("event4"),
	}

	tree, _ := NewTree(leaves)

	for i, leaf := range leaves {
		proof, err := tree.Proof(i)
		if err != nil {
			t.Fatalf("Proof(%d) failed: %v", i, err)
		}

		if !VerifyProof(leaf, proof, tree.Root()) {
			t.Errorf("proof verification failed for leaf %d", i)
		}
	}
}

func TestProofInvalidIndex(t *testing.T) {
	leaves := [][]byte{[]byte("a"), []byte("b")}
	tree, _ := NewTree(leaves)

	_, err := tree.Proof(-1)
	if err == nil {
		t.Error("expected error for negative index")
	}

	_, err = tree.Proof(2)
	if err == nil {
		t.Error("expected error for out-of-range index")
	}
}

func TestVerifyProofWrongLeaf(t *testing.T) {
	leaves := [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d")}
	tree, _ := NewTree(leaves)

	proof, _ := tree.Proof(0)

	// Verify with wrong leaf data
	if VerifyProof([]byte("wrong"), proof, tree.Root()) {
		t.Error("proof should fail for wrong leaf data")
	}
}

func TestVerifyProofWrongRoot(t *testing.T) {
	leaves := [][]byte{[]byte("a"), []byte("b")}
	tree, _ := NewTree(leaves)

	proof, _ := tree.Proof(0)

	wrongRoot := make([]byte, 32)
	if VerifyProof(leaves[0], proof, wrongRoot) {
		t.Error("proof should fail for wrong root")
	}
}

func TestHashDataConsistency(t *testing.T) {
	data := []byte("test data")
	h1 := HashData(data)
	h2 := HashData(data)

	if !equal(h1, h2) {
		t.Error("HashData should be deterministic")
	}

	if len(h1) != 32 {
		t.Errorf("SHA3-256 hash should be 32 bytes, got %d", len(h1))
	}
}

func TestLargeTree(t *testing.T) {
	// Test with 1000 leaves
	leaves := make([][]byte, 1000)
	for i := range leaves {
		leaves[i] = []byte{byte(i >> 8), byte(i)}
	}

	tree, err := NewTree(leaves)
	if err != nil {
		t.Fatalf("NewTree failed: %v", err)
	}

	if tree.LeafCount() != 1000 {
		t.Errorf("expected 1000 leaves, got %d", tree.LeafCount())
	}

	// Verify a few random proofs
	for _, idx := range []int{0, 499, 999} {
		proof, err := tree.Proof(idx)
		if err != nil {
			t.Fatalf("Proof(%d) failed: %v", idx, err)
		}
		if !VerifyProof(leaves[idx], proof, tree.Root()) {
			t.Errorf("proof verification failed for leaf %d", idx)
		}
	}
}
