package policy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/open-policy-agent/opa/v1/rego"
)

// Engine wraps an embedded OPA instance for policy evaluation.
type Engine struct {
	policyDir string
	prepared  rego.PreparedEvalQuery
	mu        sync.RWMutex
}

// AuthzInput is the input provided to the OPA policy for authorization.
type AuthzInput struct {
	User          string            `json:"user"`
	DID           string            `json:"did"`
	Role          string            `json:"role"`
	Authenticated bool              `json:"authenticated"`
	NASAddress    string            `json:"nas_address,omitempty"`
	Resource      string            `json:"resource,omitempty"`
	Timestamp     time.Time         `json:"timestamp"`
	Attributes    map[string]string `json:"attributes,omitempty"`
}

// AuthzResult is the result of a policy evaluation.
type AuthzResult struct {
	Allow       bool     `json:"allow"`
	DenyReasons []string `json:"deny_reasons,omitempty"`
}

// NewEngine creates a new OPA policy engine by loading all .rego files
// from the given directory.
func NewEngine(policyDir string) (*Engine, error) {
	e := &Engine{
		policyDir: policyDir,
	}

	if err := e.load(); err != nil {
		return nil, err
	}

	return e, nil
}

// load reads and compiles all .rego files from the policy directory.
func (e *Engine) load() error {
	// Find all .rego files
	pattern := filepath.Join(e.policyDir, "*.rego")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to glob policy files: %w", err)
	}

	if len(files) == 0 {
		return fmt.Errorf("no policy files found in %s", e.policyDir)
	}

	// Read policy modules
	modules := make([]func(*rego.Rego), 0, len(files)+1)
	modules = append(modules, rego.Query("data.soholink.authz"))

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("failed to read policy file %s: %w", f, err)
		}
		name := filepath.Base(f)
		modules = append(modules, rego.Module(name, string(data)))
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

// Reload re-reads policies from disk. Can be called on SIGHUP or via CLI.
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

	result := &AuthzResult{
		Allow: false,
	}

	if len(results) == 0 {
		result.DenyReasons = []string{"no_policy_result"}
		return result, nil
	}

	// Extract the result from the evaluation
	// The query "data.soholink.authz" returns the full authz object
	if len(results) > 0 && len(results[0].Expressions) > 0 {
		expr := results[0].Expressions[0].Value
		if resultMap, ok := expr.(map[string]interface{}); ok {
			// Check "allow" field
			if allow, ok := resultMap["allow"].(bool); ok {
				result.Allow = allow
			}

			// Check "deny_reasons" field
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

// PolicyFiles returns the list of .rego files in the policy directory.
func (e *Engine) PolicyFiles() ([]string, error) {
	pattern := filepath.Join(e.policyDir, "*.rego")
	return filepath.Glob(pattern)
}

// PolicyDir returns the policy directory path.
func (e *Engine) PolicyDir() string {
	return e.policyDir
}
