package protocoladapter

import (
	"fmt"

	"github.com/NTARI-RAND/sohocloud-protocol/listing"
)

// nodeClassForComputeClass maps the protocol's coarse ComputeClass tiers onto
// SoHoLINK's certified node_class enum. TRANSITIONAL and ordinal-preserving,
// NOT semantically equivalent: the mapping keeps the scheduler's classScore
// ordering (server > standard > micro ⇒ A > B > C) so protocol-listed nodes
// rank sensibly, but SoHoLINK's A/B/C/D classes carry uptime certifications
// the listing does not attest. Class D (storage appliances) has no protocol
// counterpart and is never produced here.
func nodeClassForComputeClass(c listing.ComputeClass) (string, error) {
	switch c {
	case listing.ClassServer:
		return "A", nil
	case listing.ClassStandard:
		return "B", nil
	case listing.ClassMicro:
		return "C", nil
	default:
		return "", fmt.Errorf("protocoladapter: unknown compute class %q", c)
	}
}
