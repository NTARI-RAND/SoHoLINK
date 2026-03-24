package policy

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/open-policy-agent/opa/v1/rego"
)

// Engine wraps an embedded OPA instance for policy evaluation.
type Engine struct {
	policyDir string
	fallback  fs.FS // used when policyDir is empty; supplied by the caller of NewEngine
	prepared  rego.PreparedEvalQuery
	mu        sync.RWMutex
}

// AuthzInput is the input provided to the OPA policy for authorization and workload scheduling.
type AuthzInput struct {
	// Authentication fields
	User          string            `json:"user"`
	DID           string            `json:"did"`
	Role          string            `json:"role"`
	Authenticated bool              `json:"authenticated"`
	NASAddress    string            `json:"nas_address,omitempty"`
	Resource      string            `json:"resource,omitempty"`
	Timestamp     time.Time         `json:"timestamp"`
	Attributes    map[string]string `json:"attributes,omitempty"`

	// Workload scheduling fields (for job placement decisions)
	WorkloadID       string  `json:"workload_id,omitempty"`        // Job identifier
	WorkloadType     string  `json:"workload_type,omitempty"`      // "batch", "inference", "encoding"
	RequesterDID     string  `json:"requester_did,omitempty"`      // Who's requesting the job
	EstimatedDuration int64  `json:"estimated_duration_seconds,omitempty"` // Estimated job duration
	RequiredCPU      float64 `json:"required_cpu,omitempty"`       // CPU cores needed
	RequiredMemoryMB int64   `json:"required_memory_mb,omitempty"` // Memory needed

	// Node state fields (for placement evaluation)
	Node *NodeState `json:"node,omitempty"`
}

// NodeState represents the current state of a node for scheduling decisions.
type NodeState struct {
	NodeDID           string  `json:"node_did"`
	AvailableCPU      float64 `json:"available_cpu"`
	AvailableMemoryMB int64   `json:"available_memory_mb"`
	GPUTemperature    float32 `json:"gpu_temperature"`      // Current GPU temp in Celsius
	GPULoad           string  `json:"gpu_load"`             // "idle", "low", "medium", "high"
	IsGPUNode         bool    `json:"is_gpu_node"`
	CurrentJobCount   int     `json:"current_job_count"`
	UptimeSecs        int64   `json:"uptime_secs"`
}

// AuthzResult is the result of a policy evaluation.
type AuthzResult struct {
	Allow       bool     `json:"allow"`
	DenyReasons []string `json:"deny_reasons,omitempty"`
}

// SchedulingResult is the result of a workload scheduling policy evaluation.
type SchedulingResult struct {
	AllowPlacement bool     `json:"allow_placement"`
	DenyReasons    []string `json:"deny_reasons,omitempty"`
}

// NewEngine creates a new OPA policy engine.
//
// policyDir — when non-empty, .rego files are loaded from disk.
//
// fallback  — when policyDir is empty, .rego files are loaded from this fs.FS
// (typically fs.Sub of the binary's embedded configs/policies tree).
// Pass nil only when policyDir is guaranteed to be non-empty at runtime;
// if both are absent NewEngine returns an error immediately.
//
// Supplying fallback as an explicit argument rather than a package-level global
// makes missing wiring visible at every call site — a nil argument is obvious
// in code review in a way that a forgotten SetEmbeddedFS() call is not.
func NewEngine(policyDir string, fallback fs.FS) (*Engine, error) {
	e := &Engine{
		policyDir: policyDir,
		fallback:  fallback,
	}

	if err := e.load(); err != nil {
		return nil, err
	}

	return e, nil
}

// load reads and compiles all .rego files from the configured source.
// _test.rego files are always skipped (OPA test helpers, not auth policies).
func (e *Engine) load() error {
	modules := []func(*rego.Rego){rego.Query("data.soholink.authz")}

	if e.policyDir != "" {
		// ── Disk path ────────────────────────────────────────────────────────
		pattern := filepath.Join(e.policyDir, "*.rego")
		files, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("failed to glob policy files: %w", err)
		}

		for _, f := range files {
			if strings.HasSuffix(f, "_test.rego") {
				continue // skip OPA test helpers
			}
			data, err := os.ReadFile(f)
			if err != nil {
				return fmt.Errorf("failed to read policy file %s: %w", f, err)
			}
			modules = append(modules, rego.Module(filepath.Base(f), string(data)))
		}

		if len(modules) == 1 { // only the query was added
			return fmt.Errorf("no policy files found in %s", e.policyDir)
		}
	} else if e.fallback != nil {
		// ── Embedded FS path ─────────────────────────────────────────────────
		files, err := fs.Glob(e.fallback, "*.rego")
		if err != nil {
			return fmt.Errorf("failed to glob embedded policy files: %w", err)
		}

		for _, f := range files {
			if strings.HasSuffix(f, "_test.rego") {
				continue // skip OPA test helpers
			}
			data, err := fs.ReadFile(e.fallback, f)
			if err != nil {
				return fmt.Errorf("failed to read embedded policy file %s: %w", f, err)
			}
			modules = append(modules, rego.Module(filepath.Base(f), string(data)))
		}

		if len(modules) == 1 {
			return fmt.Errorf("no policy files found in embedded FS")
		}
	} else {
		return fmt.Errorf("no policy source configured: policyDir is empty and no fallback fs.FS was provided to NewEngine")
	}

	// Compile and prepare
	r := rego.New(modules...)
	prepared, err := r.PrepareForEval(context.Background())
	if err != nil {
		return fmt.Errorf("failed to compile policies: %w", err)
	}

	e.mu.Lock()
	e.prepared = prepared
	e.mu.Unlock()

	return nil
}

// Reload re-reads policies from the same source (disk or embedded). Useful on
// SIGHUP or via a CLI reload command.
func (e *Engine) Reload() error {
	return e.load()
}

// Evaluate runs the authorization policy with the given input and returns the decision.
func (e *Engine) Evaluate(ctx context.Context, input *AuthzInput) (*AuthzResult, error) {
	e.mu.RLock()
	prepared := e.prepared
	e.mu.RUnlock()

	results, err := prepared.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return nil, fmt.Errorf("policy evaluation failed: %w", err)
	}

	result := &AuthzResult{Allow: false}

	if len(results) == 0 {
		result.DenyReasons = []string{"no_policy_result"}
		return result, nil
	}

	if len(results[0].Expressions) > 0 {
		expr := results[0].Expressions[0].Value
		if resultMap, ok := expr.(map[string]interface{}); ok {
			if allow, ok := resultMap["allow"].(bool); ok {
				result.Allow = allow
			}
			if reasons, ok := resultMap["deny_reasons"]; ok {
				if reasonSet, ok := reasons.([]interface{}); ok {
					for _, r := range reasonSet {
						if s, ok := r.(string); ok {
							result.DenyReasons = append(result.DenyReasons, s)
						}
					}
				}
			}
		}
	}

	return result, nil
}

// EvaluateWorkloadScheduling evaluates workload scheduling policies.
// Returns a SchedulingResult indicating whether a job can be placed on a node
// based on provider-defined thermal budgets and resource constraints.
// Uses the soholink.scheduling package from the loaded .rego files.
func (e *Engine) EvaluateWorkloadScheduling(ctx context.Context, input *AuthzInput) (*SchedulingResult, error) {
	e.mu.RLock()
	prepared := e.prepared
	e.mu.RUnlock()

	results, err := prepared.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return nil, fmt.Errorf("workload scheduling policy evaluation failed: %w", err)
	}

	result := &SchedulingResult{AllowPlacement: true} // default to allow

	if len(results) == 0 {
		return result, nil
	}

	// Look for scheduling policy results in the soholink.scheduling package
	if len(results[0].Expressions) > 0 {
		expr := results[0].Expressions[0].Value
		if resultMap, ok := expr.(map[string]interface{}); ok {
			// Check allow_placement
			if allowPlacement, ok := resultMap["allow_placement"].(bool); ok {
				result.AllowPlacement = allowPlacement
			}
			// Check deny_reasons
			if reasons, ok := resultMap["deny_reasons"]; ok {
				if reasonSet, ok := reasons.([]interface{}); ok {
					for _, r := range reasonSet {
						if s, ok := r.(string); ok {
							result.DenyReasons = append(result.DenyReasons, s)
						}
					}
				}
			}
		}
	}

	return result, nil
}

// PolicyFiles returns the list of .rego files currently in use.
func (e *Engine) PolicyFiles() ([]string, error) {
	if e.policyDir != "" {
		pattern := filepath.Join(e.policyDir, "*.rego")
		return filepath.Glob(pattern)
	}
	if e.fallback != nil {
		return fs.Glob(e.fallback, "*.rego")
	}
	return nil, nil
}

// PolicyDir returns the on-disk policy directory path, or "(embedded)" if the
// binary is running from its embedded policy set.
func (e *Engine) PolicyDir() string {
	if e.policyDir != "" {
		return e.policyDir
	}
	return "(embedded)"
}
