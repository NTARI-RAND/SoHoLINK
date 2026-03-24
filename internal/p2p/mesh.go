// Package p2p implements small-world LAN peer discovery for SoHoLINK.
//
// Each node broadcasts a signed Announcement over a private multicast group
// (239.255.42.99:7946) every 10 seconds.  Receivers verify the Ed25519
// signature, check the anti-replay timestamp, and upsert the peer into the
// federation_nodes store table — making it immediately visible to
// orchestration.NodeDiscovery without any manual configuration.
//
// Network topology: on a LAN, nodes form a tightly-clustered local workgroup
// (high clustering coefficient).  Cross-subnet WAN peers can be added via the
// HTTP registration API (/api/peers/register), providing the sparse long-range
// links that give the overall federation its small-world properties.
package p2p

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

const (
	// multicastGroup is a private multicast address reserved for SoHoLINK LAN
	// peer discovery.  It falls within the administratively-scoped range
	// 239.0.0.0/8 (RFC 2365) so it will not leave the local network.
	multicastGroup = "239.255.42.99"
	multicastPort  = 7946
	multicastAddr  = "239.255.42.99:7946"

	announceInterval = 10 * time.Second
	peerTTL          = 45 * time.Second // ~4.5× announce interval; tolerates packet loss
	staleCheckPeriod = 15 * time.Second

	// maxAnnouncementBytes caps incoming UDP datagrams to prevent amplification.
	maxAnnouncementBytes = 4096

	// replayWindowNS is the maximum allowed age of an announcement timestamp.
	// Announcements older than this are rejected as potential replays.
	replayWindowNS = int64(30 * time.Second)

	protocolVersion = 1
)

// Announcement is the signed payload broadcast by each node every
// announceInterval.  The Sig field is an Ed25519 signature over the canonical
// JSON of the struct with Sig set to nil — sign first, then attach signature.
type Announcement struct {
	V       int     `json:"v"`            // protocol version
	DID     string  `json:"did"`          // node DID (did:key:...)
	PubKey  []byte  `json:"pk"`           // Ed25519 public key, 32 bytes
	APIAddr string  `json:"api"`          // host:port for HTTP API (8080)
	IPFS    string  `json:"ipfs"`         // host:port for IPFS API (5001); may be ""
	CPU     float64 `json:"cpu"`          // total CPU cores offered
	RAMGB   float64 `json:"ram"`          // total RAM in GB offered
	DiskGB  int64   `json:"disk"`         // total storage in GB offered
	GPU     string  `json:"gpu"`          // GPU model string or ""
	Region  string  `json:"region"`       // region hint, e.g. "us-east-1"
	TS      int64   `json:"ts"`           // Unix nanoseconds (anti-replay)
	Sig     []byte  `json:"sig,omitempty"` // Ed25519 signature; nil when computing payload
}

// signingPayload returns the canonical JSON over which the signature is
// computed: the announcement with Sig zeroed so the signature covers all
// semantic fields.
func signingPayload(a *Announcement) ([]byte, error) {
	cp := *a
	cp.Sig = nil
	return json.Marshal(&cp)
}

// Sign populates a.Sig with an Ed25519 signature over the announcement body.
func (a *Announcement) Sign(priv ed25519.PrivateKey) error {
	payload, err := signingPayload(a)
	if err != nil {
		return fmt.Errorf("p2p: marshal for signing: %w", err)
	}
	a.Sig = ed25519.Sign(priv, payload)
	return nil
}

// Verify checks the Ed25519 signature and the anti-replay timestamp.
// It returns an error if either check fails.
func (a *Announcement) Verify() error {
	if len(a.PubKey) != ed25519.PublicKeySize {
		return errors.New("p2p: invalid public key length")
	}
	now := time.Now().UnixNano()
	skew := a.TS - now
	if skew < 0 {
		skew = -skew
	}
	if skew > replayWindowNS {
		return fmt.Errorf("p2p: announcement timestamp skew %v exceeds window", time.Duration(skew))
	}
	payload, err := signingPayload(a)
	if err != nil {
		return fmt.Errorf("p2p: marshal for verify: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(a.PubKey), payload, a.Sig) {
		return errors.New("p2p: signature verification failed")
	}
	return nil
}

// Peer is a discovered, verified SoHoLINK peer.
type Peer struct {
	DID                string
	PubKey             []byte
	APIAddr            string
	IPFSAddr           string
	CPU                float64
	RAMGB              float64
	DiskGB             int64
	GPU                string
	GPUVRAMFree        int64   // MB
	GPUVRAMTotal       int64   // MB
	GPUComputeCapability string // e.g. "8.6", "9.0"
	GPUTemperature     float32 // Celsius
	GPUPCIeBandwidth   int64   // MB/s
	Region             string
	LastSeen           time.Time
}

// Config holds the local node's identity and capabilities for announcement.
type Config struct {
	DID        string
	PrivateKey ed25519.PrivateKey // 64-byte Ed25519 private key
	APIAddr    string             // this node's HTTP API address (host:port)
	IPFSAddr   string             // this node's IPFS API address, or ""
	CPU        float64
	RAMGB      float64
	DiskGB     int64
	GPU        string
	GPUVRAMFree        int64   // MB
	GPUVRAMTotal       int64   // MB
	GPUComputeCapability string // e.g. "8.6", "9.0"
	GPUTemperature     float32 // Celsius
	GPUPCIeBandwidth   int64   // MB/s
	Region     string
	Store      *store.Store // if non-nil, discovered peers are persisted

	// AllowedNodeDIDs is an optional peer allowlist.  When non-empty, only
	// announcements from DIDs in this slice are accepted; all others are
	// silently dropped.  Leave nil or empty to allow all verified peers.
	AllowedNodeDIDs []string

	// AllowedCIDRs is an optional source-IP allowlist (T-007).  When non-empty,
	// multicast packets from IPs outside these CIDR ranges are silently dropped
	// before signature verification.  RFC 1918 ranges are recommended defaults
	// for home and small-office deployments.  Leave nil or empty to accept
	// packets from any source address (not recommended for production).
	AllowedCIDRs []string
}

// Mesh manages LAN peer discovery via signed multicast UDP announcements.
// It is safe for concurrent use after Start is called.
type Mesh struct {
	cfg Config

	mu    sync.RWMutex
	peers map[string]*Peer // keyed by DID

	// allowedNets is the parsed form of cfg.AllowedCIDRs (T-007).
	// Built once in New() and read-only thereafter.
	allowedNets []*net.IPNet

	// multicastLimiters is a per-source-IP token-bucket rate limiter map (T-009).
	// Using sync.Map avoids a global lock on the hot datagram receive path.
	// Each limiter allows 5 packets/second with a burst of 5.
	multicastLimiters sync.Map // map[string]*rate.Limiter

	// onPeer is called (under no lock) each time a new or updated peer is seen.
	onPeer func(*Peer)
}

// isAllowed returns true if did is permitted to join this mesh.
// When cfg.AllowedNodeDIDs is empty the mesh accepts all verified peers.
func (m *Mesh) isAllowed(did string) bool {
	if len(m.cfg.AllowedNodeDIDs) == 0 {
		return true
	}
	for _, allowed := range m.cfg.AllowedNodeDIDs {
		if allowed == did {
			return true
		}
	}
	return false
}

// New creates a Mesh with the provided configuration.
// Call Start to begin announcing and listening.
func New(cfg Config) *Mesh {
	// Pre-parse CIDR allowlist so handleDatagram does not allocate on the
	// hot path.  Malformed entries are logged and skipped.
	var allowedNets []*net.IPNet
	for _, cidr := range cfg.AllowedCIDRs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			log.Printf("[p2p] invalid AllowedCIDR %q: %v (skipping)", cidr, err)
			continue
		}
		allowedNets = append(allowedNets, ipNet)
	}

	return &Mesh{
		cfg:         cfg,
		peers:       make(map[string]*Peer),
		allowedNets: allowedNets,
	}
}

// OnPeer registers a callback invoked whenever a peer announcement is
// received and verified.  Must be called before Start.
func (m *Mesh) OnPeer(fn func(*Peer)) {
	m.onPeer = fn
}

// Start launches the announce and listen goroutines.
// It returns when ctx is cancelled.
func (m *Mesh) Start(ctx context.Context) error {
	// Resolve multicast group.
	group := &net.UDPAddr{IP: net.ParseIP(multicastGroup), Port: multicastPort}

	// Open a sending socket.
	sendConn, err := net.DialUDP("udp4", nil, group)
	if err != nil {
		return fmt.Errorf("p2p: dial multicast: %w", err)
	}
	defer sendConn.Close()

	// Open a receiving socket bound to the multicast port on all interfaces.
	recvConn, err := net.ListenMulticastUDP("udp4", nil, group)
	if err != nil {
		return fmt.Errorf("p2p: listen multicast: %w", err)
	}
	defer recvConn.Close()

	if err := recvConn.SetReadBuffer(maxAnnouncementBytes * 16); err != nil {
		log.Printf("[p2p] SetReadBuffer: %v (non-fatal)", err)
	}

	log.Printf("[p2p] mesh started (group=%s, api=%s, did=%s…)",
		multicastAddr, m.cfg.APIAddr, truncDID(m.cfg.DID))

	// Announce immediately so peers don't have to wait up to announceInterval.
	m.sendAnnouncement(sendConn)

	// Announce ticker.
	announceTick := time.NewTicker(announceInterval)
	defer announceTick.Stop()

	// Stale peer reaper.
	staleTick := time.NewTicker(staleCheckPeriod)
	defer staleTick.Stop()

	// Receive loop in a goroutine; errors sent back via channel.
	recvErr := make(chan error, 1)
	go func() {
		buf := make([]byte, maxAnnouncementBytes)
		for {
			n, src, err := recvConn.ReadFromUDP(buf)
			if err != nil {
				// Check if context was cancelled — expected shutdown path.
				select {
				case <-ctx.Done():
					return
				default:
				}
				recvErr <- err
				return
			}
			m.handleDatagram(ctx, buf[:n], src)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[p2p] mesh stopping")
			return nil
		case err := <-recvErr:
			return fmt.Errorf("p2p: receive loop: %w", err)
		case <-announceTick.C:
			m.sendAnnouncement(sendConn)
		case <-staleTick.C:
			m.reapStalePeers(ctx)
		}
	}
}

// Peers returns a snapshot of all currently live peers (LastSeen within TTL).
func (m *Mesh) Peers() []Peer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Peer, 0, len(m.peers))
	for _, p := range m.peers {
		out = append(out, *p)
	}
	return out
}

// PeerCount returns the number of live peers.
func (m *Mesh) PeerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.peers)
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func (m *Mesh) sendAnnouncement(conn *net.UDPConn) {
	a := &Announcement{
		V:       protocolVersion,
		DID:     m.cfg.DID,
		PubKey:  m.cfg.PrivateKey.Public().(ed25519.PublicKey),
		APIAddr: m.cfg.APIAddr,
		IPFS:    m.cfg.IPFSAddr,
		CPU:     m.cfg.CPU,
		RAMGB:   m.cfg.RAMGB,
		DiskGB:  m.cfg.DiskGB,
		GPU:     m.cfg.GPU,
		Region:  m.cfg.Region,
		TS:      time.Now().UnixNano(),
	}
	if err := a.Sign(m.cfg.PrivateKey); err != nil {
		log.Printf("[p2p] sign announcement: %v", err)
		return
	}
	data, err := json.Marshal(a)
	if err != nil {
		log.Printf("[p2p] marshal announcement: %v", err)
		return
	}
	if _, err := conn.Write(data); err != nil {
		log.Printf("[p2p] send announcement: %v", err)
	}
}

func (m *Mesh) handleDatagram(_ context.Context, data []byte, src *net.UDPAddr) {
	// S2-1: CIDR allowlist — drop packets from outside permitted source ranges
	// before any JSON parsing or signature verification (T-007).
	if len(m.allowedNets) > 0 {
		inAllowlist := false
		for _, n := range m.allowedNets {
			if n.Contains(src.IP) {
				inAllowlist = true
				break
			}
		}
		if !inAllowlist {
			return // silently drop — do not log to avoid amplifying attacker visibility
		}
	}

	// S2-2: Per-source-IP rate limit — drop multicast flood from a single IP
	// without processing (T-009).  5 packets/second with burst of 5.
	limiterVal, _ := m.multicastLimiters.LoadOrStore(
		src.IP.String(),
		rate.NewLimiter(rate.Every(time.Second), 5),
	)
	if !limiterVal.(*rate.Limiter).Allow() {
		return // silently drop
	}

	var a Announcement
	if err := json.Unmarshal(data, &a); err != nil {
		return // malformed — ignore silently
	}

	// Drop our own announcements (same DID).
	if a.DID == m.cfg.DID {
		return
	}

	if a.V != protocolVersion {
		return // future or old protocol version
	}

	// DID allowlist check: drop announcements from peers not in the allowlist.
	// Performed before Verify() to avoid wasting CPU on signature verification
	// for disallowed peers (though the cost difference is minimal).
	if !m.isAllowed(a.DID) {
		log.Printf("[p2p] dropping announcement from unlisted DID %s", truncDID(a.DID))
		return
	}

	if err := a.Verify(); err != nil {
		log.Printf("[p2p] dropping announcement from %s: %v", src, err)
		return
	}

	// Resolve the API address: prefer the announced address, fall back to
	// the source IP with the announced port if the announcement used a
	// placeholder like 0.0.0.0.
	apiAddr := a.APIAddr
	if host, port, err := net.SplitHostPort(apiAddr); err == nil {
		ip := net.ParseIP(host)
		if ip == nil || ip.IsUnspecified() {
			apiAddr = net.JoinHostPort(src.IP.String(), port)
		}
	}

	peer := &Peer{
		DID:      a.DID,
		PubKey:   a.PubKey,
		APIAddr:  apiAddr,
		IPFSAddr: a.IPFS,
		CPU:      a.CPU,
		RAMGB:    a.RAMGB,
		DiskGB:   a.DiskGB,
		GPU:      a.GPU,
		Region:   a.Region,
		LastSeen: time.Now(),
	}

	m.mu.Lock()
	_, isNew := m.peers[peer.DID]
	isNew = !isNew
	m.peers[peer.DID] = peer
	m.mu.Unlock()

	if isNew {
		log.Printf("[p2p] new peer: %s  api=%s  cpu=%.1f  ram=%.1fGB  disk=%dGB",
			truncDID(peer.DID), peer.APIAddr, peer.CPU, peer.RAMGB, peer.DiskGB)
	}

	// Persist to store so NodeDiscovery picks it up.
	if m.cfg.Store != nil {
		row := peerToStoreRow(peer)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := m.cfg.Store.UpsertFederationNode(ctx, row); err != nil {
			log.Printf("[p2p] upsert peer %s: %v", truncDID(peer.DID), err)
		}
	}

	if m.onPeer != nil {
		m.onPeer(peer)
	}
}

func (m *Mesh) reapStalePeers(ctx context.Context) {
	cutoff := time.Now().Add(-peerTTL)

	m.mu.Lock()
	var stale []string
	for did, p := range m.peers {
		if p.LastSeen.Before(cutoff) {
			stale = append(stale, did)
			delete(m.peers, did)
		}
	}
	m.mu.Unlock()

	if len(stale) == 0 {
		return
	}

	log.Printf("[p2p] %d stale peer(s) expired", len(stale))

	if m.cfg.Store != nil {
		for _, did := range stale {
			row := &store.FederationNodeRow{
				NodeDID:       did,
				Status:        "offline",
				LastHeartbeat: time.Now(),
			}
			tctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			if err := m.cfg.Store.UpsertFederationNode(tctx, row); err != nil {
				log.Printf("[p2p] mark offline %s: %v", truncDID(did), err)
			}
			cancel()
		}
	}
}

func peerToStoreRow(p *Peer) *store.FederationNodeRow {
	return &store.FederationNodeRow{
		NodeDID:           p.DID,
		Address:           p.APIAddr,
		Region:            p.Region,
		TotalCPU:          p.CPU,
		AvailableCPU:      p.CPU,
		TotalMemoryMB:     int64(p.RAMGB * 1024),
		AvailableMemoryMB: int64(p.RAMGB * 1024),
		TotalDiskGB:       p.DiskGB,
		AvailableDiskGB:   p.DiskGB,
		GPUModel:          p.GPU,
		ReputationScore:   50, // neutral until LBTAS scores it
		Status:            "online",
		LastHeartbeat:     p.LastSeen,
	}
}

func truncDID(did string) string {
	if len(did) > 24 {
		return did[:24] + "…"
	}
	return did
}
