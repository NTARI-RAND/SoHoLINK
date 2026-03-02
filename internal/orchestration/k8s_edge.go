// Package orchestration — Kubernetes edge adapter.
//
// K8sEdgeCluster lets the FedScheduler deploy workloads onto a real Kubernetes
// cluster at a geographic edge. It drives the Kubernetes API Server over HTTPS
// using only stdlib (net/http + encoding/json). No k8s.io/client-go is needed.
//
// Each geographic edge region maps to one K8sEdgeCluster. The FedScheduler
// calls Deploy/Delete; the cluster translates those into Kubernetes Deployments.
//
// Kubeconfig bootstrap:
//
//	cluster := NewK8sEdgeCluster(K8sEdgeConfig{
//	    Region:     "us-east-1",
//	    APIServer:  "https://k8s.us-east-1.example.com",
//	    BearerToken: os.Getenv("K8S_TOKEN"),
//	    Namespace:   "soholink",
//	    Lat: 40.71, Lon: -74.01,
//	})
package orchestration

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// K8sEdgeConfig contains the connection details for a Kubernetes edge cluster.
type K8sEdgeConfig struct {
	// Region is the geographic region label (e.g. "us-east-1", "eu-west-2").
	Region string
	// APIServer is the full URL of the kube-apiserver (e.g. "https://1.2.3.4:6443").
	APIServer string
	// BearerToken is the service-account or user token for authentication.
	BearerToken string
	// CACert is the PEM-encoded CA certificate for TLS verification (optional).
	// Leave empty to skip TLS verification (dev/testing only).
	CACert string
	// Namespace to deploy workloads into.
	Namespace string
	// Geographic coordinates of this edge cluster.
	Lat float64
	Lon float64
}

// K8sEdgeCluster manages workload deployments on a Kubernetes cluster.
type K8sEdgeCluster struct {
	cfg    K8sEdgeConfig
	client *http.Client
}

// NewK8sEdgeCluster creates a new Kubernetes edge cluster adapter.
// If cfg.CACert is set it is used to verify the server certificate.
// Without a CA cert the system root pool is used (suitable for public CAs).
// Never sets InsecureSkipVerify — callers must provide a CA cert for
// self-signed clusters.
func NewK8sEdgeCluster(cfg K8sEdgeConfig) (*K8sEdgeCluster, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}

	if cfg.CACert != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(cfg.CACert)) {
			return nil, fmt.Errorf("k8s edge %s: CACert is set but could not be parsed as PEM", cfg.Region)
		}
		tlsCfg.RootCAs = pool
	}

	transport := &http.Transport{
		TLSHandshakeTimeout: 15 * time.Second,
		TLSClientConfig:     tlsCfg,
	}

	return &K8sEdgeCluster{
		cfg: cfg,
		client: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
	}, nil
}

// Region returns the geographic region label for this cluster.
func (c *K8sEdgeCluster) Region() string { return c.cfg.Region }

// Coordinates returns the geographic position of this edge cluster.
func (c *K8sEdgeCluster) Coordinates() (lat, lon float64) {
	return c.cfg.Lat, c.cfg.Lon
}

// Deploy creates or replaces a Kubernetes Deployment for a SoHoLINK workload.
// The deployment name is derived from the placementID.
func (c *K8sEdgeCluster) Deploy(ctx context.Context, p *DeployRequest) error {
	name := k8sName(p.PlacementID)
	ns := c.cfg.Namespace

	manifest := c.buildDeployment(name, p)
	body, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("k8s deploy marshal: %w", err)
	}

	// Try server-side apply (PATCH) first; fall back to POST if not found.
	url := fmt.Sprintf("%s/apis/apps/v1/namespaces/%s/deployments/%s", c.cfg.APIServer, ns, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/apply-patch+yaml")
	req.Header.Set("Authorization", "Bearer "+c.cfg.BearerToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("k8s deploy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Resource doesn't exist yet — create it.
		createURL := fmt.Sprintf("%s/apis/apps/v1/namespaces/%s/deployments", c.cfg.APIServer, ns)
		req2, _ := http.NewRequestWithContext(ctx, http.MethodPost, createURL, bytes.NewReader(body))
		req2.Header.Set("Content-Type", "application/json")
		req2.Header.Set("Authorization", "Bearer "+c.cfg.BearerToken)
		resp2, err := c.client.Do(req2)
		if err != nil {
			return fmt.Errorf("k8s create deploy: %w", err)
		}
		defer resp2.Body.Close()
		if resp2.StatusCode < 200 || resp2.StatusCode >= 300 {
			b, _ := io.ReadAll(io.LimitReader(resp2.Body, 1024))
			return fmt.Errorf("k8s create deploy: HTTP %d: %s", resp2.StatusCode, b)
		}
		log.Printf("[k8s/%s] created deployment %s", c.cfg.Region, name)
		return nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("k8s patch deploy: HTTP %d: %s", resp.StatusCode, b)
	}
	log.Printf("[k8s/%s] applied deployment %s", c.cfg.Region, name)
	return nil
}

// Delete removes the Kubernetes Deployment for a placement.
func (c *K8sEdgeCluster) Delete(ctx context.Context, placementID string) error {
	name := k8sName(placementID)
	ns := c.cfg.Namespace
	url := fmt.Sprintf("%s/apis/apps/v1/namespaces/%s/deployments/%s", c.cfg.APIServer, ns, name)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.BearerToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("k8s delete: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil // already gone
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("k8s delete: HTTP %d: %s", resp.StatusCode, b)
	}
	log.Printf("[k8s/%s] deleted deployment %s", c.cfg.Region, name)
	return nil
}

// Status returns the ready / total replica count for a deployment.
func (c *K8sEdgeCluster) Status(ctx context.Context, placementID string) (ready, total int, err error) {
	name := k8sName(placementID)
	ns := c.cfg.Namespace
	url := fmt.Sprintf("%s/apis/apps/v1/namespaces/%s/deployments/%s", c.cfg.APIServer, ns, name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.BearerToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("k8s status: %w", err)
	}
	defer resp.Body.Close()

	var obj struct {
		Status struct {
			Replicas      int `json:"replicas"`
			ReadyReplicas int `json:"readyReplicas"`
		} `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&obj); err != nil {
		return 0, 0, fmt.Errorf("k8s status decode: %w", err)
	}
	return obj.Status.ReadyReplicas, obj.Status.Replicas, nil
}

// buildDeployment constructs the Kubernetes Deployment manifest object.
func (c *K8sEdgeCluster) buildDeployment(name string, p *DeployRequest) map[string]any {
	replicas := 1
	cpuReq := fmt.Sprintf("%.0fm", p.Spec.CPUCores*1000)  // millicores
	memReq := fmt.Sprintf("%dMi", p.Spec.MemoryMB)

	env := make([]map[string]any, 0, len(p.Spec.Environment))
	for k, v := range p.Spec.Environment {
		env = append(env, map[string]any{"name": k, "value": v})
	}

	ports := make([]map[string]any, 0, len(p.Spec.Ports))
	for _, pm := range p.Spec.Ports {
		ports = append(ports, map[string]any{
			"containerPort": pm.ContainerPort,
			"protocol":      strings.ToUpper(pm.Protocol),
		})
	}

	return map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      name,
			"namespace": c.cfg.Namespace,
			"labels": map[string]any{
				"app":          "soholink",
				"workload":     p.WorkloadID,
				"placement":    p.PlacementID,
				"soholink/region": c.cfg.Region,
			},
		},
		"spec": map[string]any{
			"replicas": replicas,
			"selector": map[string]any{
				"matchLabels": map[string]any{"placement": p.PlacementID},
			},
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": map[string]any{"placement": p.PlacementID},
				},
				"spec": map[string]any{
					"containers": []map[string]any{
						{
							"name":    "workload",
							"image":   p.Spec.Image,
							"command": p.Spec.Entrypoint,
							"env":     env,
							"ports":   ports,
							"resources": map[string]any{
								"requests": map[string]any{
									"cpu":    cpuReq,
									"memory": memReq,
								},
								"limits": map[string]any{
									"cpu":    cpuReq,
									"memory": memReq,
								},
							},
						},
					},
				},
			},
		},
	}
}

// k8sName converts a placement ID to a valid Kubernetes resource name.
// K8s names must be lowercase alphanumeric / dashes, max 63 chars.
func k8sName(placementID string) string {
	name := strings.ToLower(placementID)
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, name)
	// Trim leading/trailing dashes
	name = strings.Trim(name, "-")
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

// EdgeRegistry manages multiple K8s edge clusters indexed by region.
type EdgeRegistry struct {
	clusters map[string]*K8sEdgeCluster // key: region
}

// NewEdgeRegistry creates an empty registry.
func NewEdgeRegistry() *EdgeRegistry {
	return &EdgeRegistry{clusters: make(map[string]*K8sEdgeCluster)}
}

// Register adds a cluster to the registry.
func (r *EdgeRegistry) Register(cluster *K8sEdgeCluster) {
	r.clusters[cluster.Region()] = cluster
}

// Get returns the cluster for a region, or nil if not registered.
func (r *EdgeRegistry) Get(region string) *K8sEdgeCluster {
	return r.clusters[region]
}

// Regions returns all registered region names.
func (r *EdgeRegistry) Regions() []string {
	regions := make([]string, 0, len(r.clusters))
	for r := range r.clusters {
		regions = append(regions, r)
	}
	return regions
}

// DeployToEdge routes a deploy request to the appropriate regional cluster.
// It selects the cluster matching the first region in the workload constraints.
// Falls back to the first registered cluster if no match.
func (r *EdgeRegistry) DeployToEdge(ctx context.Context, req *DeployRequest, regions []string) error {
	var cluster *K8sEdgeCluster
	for _, region := range regions {
		if c := r.clusters[region]; c != nil {
			cluster = c
			break
		}
	}
	if cluster == nil {
		// No region match — use any available cluster
		for _, c := range r.clusters {
			cluster = c
			break
		}
	}
	if cluster == nil {
		return fmt.Errorf("edge registry: no clusters registered")
	}
	return cluster.Deploy(ctx, req)
}
