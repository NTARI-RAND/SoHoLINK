// Package storage provides content-addressed storage backed by IPFS.
// The IPFSClient speaks to a locally-running Kubo daemon via its HTTP RPC API
// (default: http://127.0.0.1:5001). No extra Go dependencies are required —
// the client uses stdlib net/http only.
//
// IPFS daemon must be installed separately:
//   https://docs.ipfs.tech/install/command-line/
//
// Quick start:
//   ipfs init && ipfs daemon
package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// IPFSClient talks to a Kubo IPFS daemon over its HTTP RPC API.
type IPFSClient struct {
	apiBase    string // e.g. "http://127.0.0.1:5001/api/v0"
	httpClient *http.Client
}

// NewIPFSClient creates a client pointing at the given Kubo API base URL.
// Pass "" to use the default (http://127.0.0.1:5001/api/v0).
func NewIPFSClient(apiBase string) *IPFSClient {
	if apiBase == "" {
		apiBase = "http://127.0.0.1:5001/api/v0"
	}
	return &IPFSClient{
		apiBase: strings.TrimRight(apiBase, "/"),
		httpClient: &http.Client{
			Timeout: 5 * time.Minute, // large files can be slow
		},
	}
}

// ipfsAddResponse is the JSON returned by /api/v0/add.
type ipfsAddResponse struct {
	Name string `json:"Name"`
	Hash string `json:"Hash"`
	Size string `json:"Size"`
}

// ipfsPinResponse is the JSON returned by /api/v0/pin/add.
type ipfsPinResponse struct {
	Pins []string `json:"Pins"`
}

// Add uploads data to IPFS and returns the CID.
// The content is pinned locally so the daemon keeps it.
func (c *IPFSClient) Add(ctx context.Context, name string, r io.Reader) (cid string, err error) {
	// Build multipart form body
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		return "", fmt.Errorf("ipfs add: create form file: %w", err)
	}
	if _, err = io.Copy(fw, r); err != nil {
		return "", fmt.Errorf("ipfs add: copy: %w", err)
	}
	mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase+"/add?pin=true&quieter=true", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ipfs add: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("ipfs add: HTTP %d: %s", resp.StatusCode, body)
	}

	var result ipfsAddResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("ipfs add: decode response: %w", err)
	}
	return result.Hash, nil
}

// Get retrieves content from IPFS by CID and returns a reader.
// The caller must close the returned ReadCloser.
func (c *IPFSClient) Get(ctx context.Context, cid string) (io.ReadCloser, error) {
	u := c.apiBase + "/cat?arg=" + url.QueryEscape(cid)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ipfs cat %s: %w", cid, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("ipfs cat %s: HTTP %d", cid, resp.StatusCode)
	}
	return resp.Body, nil
}

// Pin ensures a CID is pinned on this node (prevents garbage collection).
func (c *IPFSClient) Pin(ctx context.Context, cid string) error {
	u := c.apiBase + "/pin/add?arg=" + url.QueryEscape(cid)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ipfs pin add %s: %w", cid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("ipfs pin add %s: HTTP %d: %s", cid, resp.StatusCode, body)
	}
	return nil
}

// Unpin removes a pin from a CID (allows garbage collection).
func (c *IPFSClient) Unpin(ctx context.Context, cid string) error {
	u := c.apiBase + "/pin/rm?arg=" + url.QueryEscape(cid)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ipfs pin rm %s: %w", cid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("ipfs pin rm %s: HTTP %d: %s", cid, resp.StatusCode, body)
	}
	return nil
}

// IsOnline returns true if the IPFS daemon is reachable.
func (c *IPFSClient) IsOnline(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase+"/id", nil)
	if err != nil {
		return false
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// IPFSStoragePool is a content-addressed storage pool backed by IPFS.
// Files are uploaded to IPFS; the returned CID is the stable address.
// A local metadata index maps file names / owner DIDs to CIDs.
type IPFSStoragePool struct {
	client      *IPFSClient
	maxFileSize int64
	mu          sync.RWMutex
	index       map[string]*IPFSFile // key: fileID (cid[:16]); guarded by mu
}

// IPFSFile describes a file stored in IPFS.
type IPFSFile struct {
	FileID    string    // CID[:16] short ID
	CID       string    // Full IPFS CID
	OwnerDID  string
	FileName  string
	MimeType  string
	Size      int64
	Encrypted bool
	CreatedAt time.Time
}

// NewIPFSStoragePool creates a storage pool backed by the given IPFS client.
func NewIPFSStoragePool(client *IPFSClient, maxFileSize int64) *IPFSStoragePool {
	return &IPFSStoragePool{
		client:      client,
		maxFileSize: maxFileSize,
		index:       make(map[string]*IPFSFile),
	}
}

// Upload stores a file in IPFS and returns the metadata.
func (p *IPFSStoragePool) Upload(ctx context.Context, ownerDID, fileName, mimeType string, r io.Reader) (*IPFSFile, error) {
	// Enforce size limit using a limited reader
	limited := io.LimitReader(r, p.maxFileSize+1)

	cid, err := p.client.Add(ctx, fileName, limited)
	if err != nil {
		return nil, fmt.Errorf("ipfs upload: %w", err)
	}

	fileID := cid
	if len(cid) > 16 {
		fileID = cid[:16]
	}

	f := &IPFSFile{
		FileID:    fileID,
		CID:       cid,
		OwnerDID:  ownerDID,
		FileName:  fileName,
		MimeType:  mimeType,
		CreatedAt: time.Now(),
	}
	p.mu.Lock()
	p.index[fileID] = f
	p.mu.Unlock()
	return f, nil
}

// Download retrieves a file from IPFS by its CID.
// The caller must close the returned ReadCloser.
func (p *IPFSStoragePool) Download(ctx context.Context, cid string) (io.ReadCloser, error) {
	return p.client.Get(ctx, cid)
}

// Delete unpins a file from the local IPFS node.
// The content may still exist on other nodes that have it pinned.
func (p *IPFSStoragePool) Delete(ctx context.Context, fileID string) error {
	p.mu.RLock()
	f, ok := p.index[fileID]
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("ipfs delete: file %q not found in index", fileID)
	}
	if err := p.client.Unpin(ctx, f.CID); err != nil {
		return err
	}
	p.mu.Lock()
	delete(p.index, fileID)
	p.mu.Unlock()
	return nil
}

// LookupByCID finds a file by its full CID.
func (p *IPFSStoragePool) LookupByCID(cid string) (*IPFSFile, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, f := range p.index {
		if f.CID == cid {
			return f, true
		}
	}
	return nil, false
}
