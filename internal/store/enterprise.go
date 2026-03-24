package store

import (
	"context"
	"database/sql"
	"time"
)

// ── Federation Nodes ──────────────────────────────────────────────────────────

// FederationNodeRow represents a federation node in the database.
type FederationNodeRow struct {
	NodeDID               string
	Address               string
	Region                string
	TotalCPU              float64
	AvailableCPU          float64
	TotalMemoryMB         int64
	AvailableMemoryMB     int64
	TotalDiskGB           int64
	AvailableDiskGB       int64
	GPUModel              string
	GPUVRAMFree           int64   // MB
	GPUVRAMTotal          int64   // MB
	GPUComputeCapability  string  // e.g. "8.6", "9.0"
	GPUTemperature        float32 // Celsius
	GPUPCIeBandwidth      int64   // MB/s
	PricePerCPUHour       int64
	ReputationScore       int
	UptimePercent         float64
	FailureRate           float64
	Status                string
	LastHeartbeat         time.Time
	PublicKey             string // base64-encoded Ed25519 public key (32 bytes)
}

// GetOnlineNodes returns all federation nodes with status "online".
func (s *Store) GetOnlineNodes(ctx context.Context) ([]FederationNodeRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT node_did, address, region,
			   total_cpu, available_cpu,
			   total_memory_mb, available_memory_mb,
			   total_disk_gb, available_disk_gb,
			   gpu_model, gpu_vram_free, gpu_vram_total,
			   gpu_compute_capability, gpu_temperature, gpu_pcie_bandwidth,
			   price_per_cpu_hour,
			   reputation_score, uptime_percent, failure_rate,
			   status, last_heartbeat, public_key
		FROM federation_nodes WHERE status = 'online'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []FederationNodeRow
	for rows.Next() {
		var n FederationNodeRow
		err := rows.Scan(
			&n.NodeDID, &n.Address, &n.Region,
			&n.TotalCPU, &n.AvailableCPU,
			&n.TotalMemoryMB, &n.AvailableMemoryMB,
			&n.TotalDiskGB, &n.AvailableDiskGB,
			&n.GPUModel, &n.GPUVRAMFree, &n.GPUVRAMTotal,
			&n.GPUComputeCapability, &n.GPUTemperature, &n.GPUPCIeBandwidth,
			&n.PricePerCPUHour,
			&n.ReputationScore, &n.UptimePercent, &n.FailureRate,
			&n.Status, &n.LastHeartbeat, &n.PublicKey,
		)
		if err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

// UpsertFederationNode inserts or updates a federation node.
func (s *Store) UpsertFederationNode(ctx context.Context, n *FederationNodeRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO federation_nodes (
			node_did, address, region,
			total_cpu, available_cpu,
			total_memory_mb, available_memory_mb,
			total_disk_gb, available_disk_gb,
			gpu_model, gpu_vram_free, gpu_vram_total,
			gpu_compute_capability, gpu_temperature, gpu_pcie_bandwidth,
			price_per_cpu_hour,
			reputation_score, uptime_percent, failure_rate,
			status, last_heartbeat, public_key
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(node_did) DO UPDATE SET
			address=excluded.address,
			region=excluded.region,
			total_cpu=excluded.total_cpu,
			available_cpu=excluded.available_cpu,
			total_memory_mb=excluded.total_memory_mb,
			available_memory_mb=excluded.available_memory_mb,
			total_disk_gb=excluded.total_disk_gb,
			available_disk_gb=excluded.available_disk_gb,
			gpu_model=excluded.gpu_model,
			gpu_vram_free=excluded.gpu_vram_free,
			gpu_vram_total=excluded.gpu_vram_total,
			gpu_compute_capability=excluded.gpu_compute_capability,
			gpu_temperature=excluded.gpu_temperature,
			gpu_pcie_bandwidth=excluded.gpu_pcie_bandwidth,
			price_per_cpu_hour=excluded.price_per_cpu_hour,
			reputation_score=excluded.reputation_score,
			uptime_percent=excluded.uptime_percent,
			failure_rate=excluded.failure_rate,
			status=excluded.status,
			last_heartbeat=excluded.last_heartbeat,
			public_key=excluded.public_key`,
		n.NodeDID, n.Address, n.Region,
		n.TotalCPU, n.AvailableCPU,
		n.TotalMemoryMB, n.AvailableMemoryMB,
		n.TotalDiskGB, n.AvailableDiskGB,
		n.GPUModel, n.GPUVRAMFree, n.GPUVRAMTotal,
		n.GPUComputeCapability, n.GPUTemperature, n.GPUPCIeBandwidth,
		n.PricePerCPUHour,
		n.ReputationScore, n.UptimePercent, n.FailureRate,
		n.Status, n.LastHeartbeat, n.PublicKey,
	)
	return err
}

// ── Workloads ─────────────────────────────────────────────────────────────────

// WorkloadRow represents a workload in the database.
type WorkloadRow struct {
	WorkloadID   string
	OwnerDID     string
	Name         string
	WorkloadType string
	Replicas     int
	Status       string
	Image        string
	CPUCores     float64
	MemoryMB     int64
	DiskGB       int64
	GPURequired  bool
	GPUModel     string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// CreateWorkload inserts a new workload record.
func (s *Store) CreateWorkload(ctx context.Context, w *WorkloadRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workloads (
			workload_id, owner_did, name, workload_type,
			replicas, status, image,
			cpu_cores, memory_mb, disk_gb,
			gpu_required, gpu_model,
			created_at, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		w.WorkloadID, w.OwnerDID, w.Name, w.WorkloadType,
		w.Replicas, w.Status, w.Image,
		w.CPUCores, w.MemoryMB, w.DiskGB,
		w.GPURequired, w.GPUModel,
		w.CreatedAt, w.UpdatedAt,
	)
	return err
}

// UpdateWorkloadStatus updates the status of a workload.
func (s *Store) UpdateWorkloadStatus(ctx context.Context, workloadID, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE workloads SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE workload_id = ?`,
		status, workloadID)
	return err
}

// GetWorkload retrieves a workload by ID.
func (s *Store) GetWorkload(ctx context.Context, workloadID string) (*WorkloadRow, error) {
	var w WorkloadRow
	err := s.db.QueryRowContext(ctx, `
		SELECT workload_id, owner_did, name, workload_type,
			   replicas, status, image,
			   cpu_cores, memory_mb, disk_gb,
			   gpu_required, gpu_model,
			   created_at, updated_at
		FROM workloads WHERE workload_id = ?`, workloadID).Scan(
		&w.WorkloadID, &w.OwnerDID, &w.Name, &w.WorkloadType,
		&w.Replicas, &w.Status, &w.Image,
		&w.CPUCores, &w.MemoryMB, &w.DiskGB,
		&w.GPURequired, &w.GPUModel,
		&w.CreatedAt, &w.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &w, err
}

// ── Placements ────────────────────────────────────────────────────────────────

// PlacementRow represents a workload replica placement on a federation node.
type PlacementRow struct {
	PlacementID string
	WorkloadID  string
	ReplicaNum  int
	NodeDID     string
	NodeAddress string
	Status      string
	StartedAt   time.Time
}

// CreatePlacement inserts a new placement record.
func (s *Store) CreatePlacement(ctx context.Context, p *PlacementRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO placements (
			placement_id, workload_id, replica_num,
			node_did, node_address, status, started_at
		) VALUES (?,?,?,?,?,?,?)`,
		p.PlacementID, p.WorkloadID, p.ReplicaNum,
		p.NodeDID, p.NodeAddress, p.Status, p.StartedAt,
	)
	return err
}

// GetPlacementsForWorkload returns all placements for a workload.
func (s *Store) GetPlacementsForWorkload(ctx context.Context, workloadID string) ([]PlacementRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT placement_id, workload_id, replica_num,
			   node_did, node_address, status, started_at
		FROM placements WHERE workload_id = ?
		ORDER BY replica_num`, workloadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PlacementRow
	for rows.Next() {
		var p PlacementRow
		err := rows.Scan(&p.PlacementID, &p.WorkloadID, &p.ReplicaNum,
			&p.NodeDID, &p.NodeAddress, &p.Status, &p.StartedAt)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// UpdatePlacementStatus updates the status of a placement.
func (s *Store) UpdatePlacementStatus(ctx context.Context, placementID, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE placements SET status = ? WHERE placement_id = ?`,
		status, placementID)
	return err
}

// ── Service Instances ─────────────────────────────────────────────────────────

// ServiceInstanceRow represents a managed service instance in the database.
type ServiceInstanceRow struct {
	InstanceID  string
	OwnerDID    string
	ServiceType string
	Name        string
	Plan        string
	Status      string
	NodeDID     string
	Endpoint    string
	Port        int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CreateServiceInstance inserts a new managed service instance.
func (s *Store) CreateServiceInstance(ctx context.Context, si *ServiceInstanceRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO service_instances (
			instance_id, owner_did, service_type, name, plan,
			status, node_did, endpoint, port,
			created_at, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		si.InstanceID, si.OwnerDID, si.ServiceType, si.Name, si.Plan,
		si.Status, si.NodeDID, si.Endpoint, si.Port,
		si.CreatedAt, si.UpdatedAt,
	)
	return err
}

// UpdateServiceInstanceStatus updates the status of a service instance.
func (s *Store) UpdateServiceInstanceStatus(ctx context.Context, instanceID, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE service_instances SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE instance_id = ?`,
		status, instanceID)
	return err
}

// GetServiceInstances returns all service instances for an owner.
func (s *Store) GetServiceInstances(ctx context.Context, ownerDID string) ([]ServiceInstanceRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT instance_id, owner_did, service_type, name, plan,
			   status, node_did, endpoint, port,
			   created_at, updated_at
		FROM service_instances WHERE owner_did = ?
		ORDER BY created_at DESC`, ownerDID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ServiceInstanceRow
	for rows.Next() {
		var si ServiceInstanceRow
		err := rows.Scan(&si.InstanceID, &si.OwnerDID, &si.ServiceType, &si.Name, &si.Plan,
			&si.Status, &si.NodeDID, &si.Endpoint, &si.Port,
			&si.CreatedAt, &si.UpdatedAt)
		if err != nil {
			return nil, err
		}
		result = append(result, si)
	}
	return result, rows.Err()
}

// ── SLA Contracts ─────────────────────────────────────────────────────────────

// SLAContractRow represents an SLA contract in the database.
type SLAContractRow struct {
	ContractID      string
	OwnerDID        string
	Tier            string
	Status          string
	UptimeTarget    float64
	LatencyTargetMs int
	StartDate       time.Time
	EndDate         time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// CreateSLAContract inserts a new SLA contract.
func (s *Store) CreateSLAContract(ctx context.Context, c *SLAContractRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sla_contracts (
			contract_id, owner_did, tier, status,
			uptime_target, latency_target_ms,
			start_date, end_date,
			created_at, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		c.ContractID, c.OwnerDID, c.Tier, c.Status,
		c.UptimeTarget, c.LatencyTargetMs,
		c.StartDate, c.EndDate,
		c.CreatedAt, c.UpdatedAt,
	)
	return err
}

// GetSLAContract retrieves an SLA contract by ID.
func (s *Store) GetSLAContract(ctx context.Context, contractID string) (*SLAContractRow, error) {
	var c SLAContractRow
	err := s.db.QueryRowContext(ctx, `
		SELECT contract_id, owner_did, tier, status,
			   uptime_target, latency_target_ms,
			   start_date, end_date,
			   created_at, updated_at
		FROM sla_contracts WHERE contract_id = ?`, contractID).Scan(
		&c.ContractID, &c.OwnerDID, &c.Tier, &c.Status,
		&c.UptimeTarget, &c.LatencyTargetMs,
		&c.StartDate, &c.EndDate,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &c, err
}

// ── SLA Violations ────────────────────────────────────────────────────────────

// SLAViolationRow represents an SLA violation in the database.
type SLAViolationRow struct {
	ViolationID   string
	ContractID    string
	ViolationType string
	Severity      string
	MeasuredValue float64
	TargetValue   float64
	CreditAmount  int64
	DetectedAt    time.Time
	ResolvedAt    sql.NullTime
}

// CreateSLAViolation inserts a new SLA violation.
func (s *Store) CreateSLAViolation(ctx context.Context, v *SLAViolationRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sla_violations (
			violation_id, contract_id, violation_type,
			severity, measured_value, target_value,
			credit_amount, detected_at
		) VALUES (?,?,?,?,?,?,?,?)`,
		v.ViolationID, v.ContractID, v.ViolationType,
		v.Severity, v.MeasuredValue, v.TargetValue,
		v.CreditAmount, v.DetectedAt,
	)
	return err
}

// GetSLAViolations returns violations for a contract.
func (s *Store) GetSLAViolations(ctx context.Context, contractID string) ([]SLAViolationRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT violation_id, contract_id, violation_type,
			   severity, measured_value, target_value,
			   credit_amount, detected_at, resolved_at
		FROM sla_violations WHERE contract_id = ?
		ORDER BY detected_at DESC`, contractID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SLAViolationRow
	for rows.Next() {
		var v SLAViolationRow
		err := rows.Scan(&v.ViolationID, &v.ContractID, &v.ViolationType,
			&v.Severity, &v.MeasuredValue, &v.TargetValue,
			&v.CreditAmount, &v.DetectedAt, &v.ResolvedAt)
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

// ── Blockchain Batches ────────────────────────────────────────────────────────

// BlockchainBatchRow represents a row in the blockchain_batches table.
type BlockchainBatchRow struct {
	Height     int64
	MerkleRoot []byte
	PrevHash   []byte
	Hash       []byte
	Timestamp  time.Time
	NodeDID    string
	Signature  []byte
	SourceFile string
	LeafCount  int
	TreeHeight int
}

// CreateBlockchainBatch inserts a new blockchain batch record.
func (s *Store) CreateBlockchainBatch(ctx context.Context, row *BlockchainBatchRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO blockchain_batches (height, merkle_root, prev_hash, hash, timestamp, node_did, signature, source_file, leaf_count, tree_height)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.Height, row.MerkleRoot, row.PrevHash, row.Hash, row.Timestamp, row.NodeDID, row.Signature, row.SourceFile, row.LeafCount, row.TreeHeight)
	return err
}

// GetLatestBlockchainBatch returns the most recent blockchain batch.
func (s *Store) GetLatestBlockchainBatch(ctx context.Context) (*BlockchainBatchRow, error) {
	row := &BlockchainBatchRow{}
	err := s.db.QueryRowContext(ctx,
		`SELECT height, merkle_root, prev_hash, hash, timestamp, node_did, signature, source_file, leaf_count, tree_height
		 FROM blockchain_batches ORDER BY height DESC LIMIT 1`).
		Scan(&row.Height, &row.MerkleRoot, &row.PrevHash, &row.Hash, &row.Timestamp, &row.NodeDID, &row.Signature, &row.SourceFile, &row.LeafCount, &row.TreeHeight)
	if err != nil {
		return nil, err
	}
	return row, nil
}

// GetBlockchainBatchByHash returns a blockchain batch by its hash.
func (s *Store) GetBlockchainBatchByHash(ctx context.Context, hash []byte) (*BlockchainBatchRow, error) {
	row := &BlockchainBatchRow{}
	err := s.db.QueryRowContext(ctx,
		`SELECT height, merkle_root, prev_hash, hash, timestamp, node_did, signature, source_file, leaf_count, tree_height
		 FROM blockchain_batches WHERE hash = ?`, hash).
		Scan(&row.Height, &row.MerkleRoot, &row.PrevHash, &row.Hash, &row.Timestamp, &row.NodeDID, &row.Signature, &row.SourceFile, &row.LeafCount, &row.TreeHeight)
	if err != nil {
		return nil, err
	}
	return row, nil
}

// GetBlockchainBatchByHeight returns a blockchain batch by height.
func (s *Store) GetBlockchainBatchByHeight(ctx context.Context, height int64) (*BlockchainBatchRow, error) {
	row := &BlockchainBatchRow{}
	err := s.db.QueryRowContext(ctx,
		`SELECT height, merkle_root, prev_hash, hash, timestamp, node_did, signature, source_file, leaf_count, tree_height
		 FROM blockchain_batches WHERE height = ?`, height).
		Scan(&row.Height, &row.MerkleRoot, &row.PrevHash, &row.Hash, &row.Timestamp, &row.NodeDID, &row.Signature, &row.SourceFile, &row.LeafCount, &row.TreeHeight)
	if err != nil {
		return nil, err
	}
	return row, nil
}
