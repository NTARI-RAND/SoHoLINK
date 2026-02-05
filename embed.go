package soholink

import (
	_ "embed"
)

//go:embed configs/default.yaml
var DefaultConfigYAML []byte

//go:embed configs/policies/default.rego
var DefaultPolicyRego []byte
