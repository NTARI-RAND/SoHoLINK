package merkle

import (
	"fmt"

	"golang.org/x/crypto/sha3"
)

// LeafPrefix is prepended to leaf data before hashing (Certificate Transparency convention).
var LeafPrefix = []byte{0x00}

// NodePrefix is prepended to internal node data before hashing.
var NodePrefix = []byte{0x01}

// Tree represents a binary Merkle tree built with SHA3-256.
type Tree struct {
	Leaves [][]byte
	Nodes  [][]byte // All nodes in level-order
	root   []byte
	height int
}

// NewTree constructs a Merkle tree from the given leaf data.
// Each leaf is hashed as SHA3-256(0x00 || data).
// Internal nodes are SHA3-256(0x01 || left || right).
func NewTree(leaves [][]byte) (*Tree, error) {
	if len(leaves) == 0 {
		return nil, fmt.Errorf("cannot build Merkle tree with zero leaves")
	}

	t := &Tree{
		Leaves: leaves,
	}

	// Hash all leaves
	currentLevel := make([][]byte, len(leaves))
	for i, leaf := range leaves {
		currentLevel[i] = hashLeaf(leaf)
	}

	// Build tree bottom-up
	allNodes := make([][]byte, 0)
	allNodes = append(allNodes, currentLevel...)
	height := 0

	for len(currentLevel) > 1 {
		nextLevel := make([][]byte, 0)

		for i := 0; i < len(currentLevel); i += 2 {
			if i+1 < len(currentLevel) {
				// Hash pair
				parent := hashNode(currentLevel[i], currentLevel[i+1])
				nextLevel = append(nextLevel, parent)
			} else {
				// Odd node: promote to next level
				nextLevel = append(nextLevel, currentLevel[i])
			}
		}

		allNodes = append(allNodes, nextLevel...)
		currentLevel = nextLevel
		height++
	}

	t.root = currentLevel[0]
	t.Nodes = allNodes
	t.height = height

	return t, nil
}

// Root returns the Merkle root hash.
func (t *Tree) Root() []byte {
	return t.root
}

// Height returns the height of the tree.
func (t *Tree) Height() int {
	return t.height
}

// LeafCount returns the number of leaves.
func (t *Tree) LeafCount() int {
	return len(t.Leaves)
}

// Proof generates a Merkle inclusion proof for the leaf at the given index.
// Returns a list of sibling hashes from leaf to root.
func (t *Tree) Proof(index int) ([]ProofElement, error) {
	if index < 0 || index >= len(t.Leaves) {
		return nil, fmt.Errorf("leaf index %d out of range [0, %d)", index, len(t.Leaves))
	}

	// Rebuild levels to compute proof path
	currentLevel := make([][]byte, len(t.Leaves))
	for i, leaf := range t.Leaves {
		currentLevel[i] = hashLeaf(leaf)
	}

	var proof []ProofElement
	idx := index

	for len(currentLevel) > 1 {
		var sibling []byte
		var isRight bool

		if idx%2 == 0 {
			// Current node is left child
			if idx+1 < len(currentLevel) {
				sibling = currentLevel[idx+1]
				isRight = true
			}
		} else {
			// Current node is right child
			sibling = currentLevel[idx-1]
			isRight = false
		}

		if sibling != nil {
			proof = append(proof, ProofElement{
				Hash:    sibling,
				IsRight: isRight,
			})
		}

		// Build next level
		nextLevel := make([][]byte, 0)
		for i := 0; i < len(currentLevel); i += 2 {
			if i+1 < len(currentLevel) {
				parent := hashNode(currentLevel[i], currentLevel[i+1])
				nextLevel = append(nextLevel, parent)
			} else {
				nextLevel = append(nextLevel, currentLevel[i])
			}
		}

		currentLevel = nextLevel
		idx = idx / 2
	}

	return proof, nil
}

// ProofElement represents one step in a Merkle proof.
type ProofElement struct {
	Hash    []byte `json:"hash"`
	IsRight bool   `json:"is_right"` // true if sibling is on the right
}

// VerifyProof verifies a Merkle inclusion proof.
func VerifyProof(leafData []byte, proof []ProofElement, root []byte) bool {
	current := hashLeaf(leafData)

	for _, p := range proof {
		if p.IsRight {
			current = hashNode(current, p.Hash)
		} else {
			current = hashNode(p.Hash, current)
		}
	}

	return equal(current, root)
}

// hashLeaf computes SHA3-256(LeafPrefix || data).
func hashLeaf(data []byte) []byte {
	h := sha3.New256()
	h.Write(LeafPrefix)
	h.Write(data)
	return h.Sum(nil)
}

// hashNode computes SHA3-256(NodePrefix || left || right).
func hashNode(left, right []byte) []byte {
	h := sha3.New256()
	h.Write(NodePrefix)
	h.Write(left)
	h.Write(right)
	return h.Sum(nil)
}

// HashData computes SHA3-256 of arbitrary data (for external use).
func HashData(data []byte) []byte {
	h := sha3.New256()
	h.Write(data)
	return h.Sum(nil)
}

func equal(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
