package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Row types (store-layer, no external dependencies)
// ---------------------------------------------------------------------------

// TenantRow represents a registered thin-client tenant.
type TenantRow struct {
	TenantID   string
	Name       string
	CenterDID  string
	Status     string // "active", "suspended"
	CPUCores   int
	StorageGB  int64
	MemoryGB   int64
	GPUModel   string
	LastActive time.Time
	CreatedAt  time.Time
}

// CentralRevenueRow tracks revenue splits for each transaction.
type CentralRevenueRow struct {
	RevenueID      string
	TransactionID  string
	TenantID       string
	TotalAmount    int64
	CentralFee     int64
	ProducerPayout int64
	ProcessorFee   int64
	Currency       string
	SettledAt      *time.Time
	CreatedAt      time.Time
}

// AutoAcceptRuleRow stores a rental auto-accept rule.
type AutoAcceptRuleRow struct {
	RuleID           string
	RuleName         string
	Enabled          bool
	Priority         int
	MinUserScore     int
	MaxAmount        int64
	ResourceType     string
	AllowedHoursJSON string
	AllowedDaysJSON  string
	RequirePrepay    bool
	Action           string
	NotifyOperator   bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// P2PBlockRow is a locally-produced blockchain block for offline operation.
type P2PBlockRow struct {
	Height    int64
	Data      []byte
	Hash      []byte
	PrevHash  []byte
	Timestamp time.Time
	Synced    bool
}

// P2PPeerRow represents a known peer on the P2P mesh.
type P2PPeerRow struct {
	PeerDID   string
	Address   string
	PublicKey []byte
	LastSeen  time.Time
	Score     int
	CPUCores  int
	StorageGB int64
	GPUModel  string
}

// P2PPendingSyncRow is an operation queued for sync to central.
type P2PPendingSyncRow struct {
	SyncID     string
	DataType   string
	Data       []byte
	CreatedAt  time.Time
	RetryCount int
}

// RatingAlertRow records a catastrophic-rating alert.
type RatingAlertRow struct {
	AlertID       string
	TransactionID string
	UserDID       string
	ProviderDID   string
	CenterDID     string
	AlertType     string
	Severity      string
	Evidence      []byte
	Notes         string
	Status        string
	Resolution    string
	InvestigatedBy string
	InvestigatedAt *time.Time
	ResolvedAt     *time.Time
	CreatedAt      time.Time
}

// OtherRatingRow is a minimal view of a rating used for cross-checking.
type OtherRatingRow struct {
	Score int
}

// DisputeRow records a dispute for a transaction.
type DisputeRow struct {
	DisputeID     string
	TransactionID string
	FilerDID      string
	Reason        string
	Priority      string
	Status        string
	Resolution    string
	ResolvedAt    *time.Time
	CreatedAt     time.Time
}

// InvestigationRow tracks a dispute investigation.
type InvestigationRow struct {
	InvestigationID string
	DisputeID       string
	InvestigatorDID string
	Status          string
	Findings        string
	Recommendation  string
	Deadline        time.Time
	CreatedAt       time.Time
	ResolvedAt      *time.Time
}

// CenterRatingRow is a single rating of a central SOHO's dispute handling.
type CenterRatingRow struct {
	RatingID  string
	DisputeID string
	CenterDID string
	RaterDID  string
	RaterRole string
	Score     int
	Feedback  string
	CreatedAt time.Time
	Signature []byte
}

// CenterScoreRow is the aggregate reputation of a central SOHO node.
type CenterScoreRow struct {
	CenterDID            string
	OverallScore         int
	InvestigationQuality float64
	Fairness             float64
	Timeliness           float64
	Communication        float64
	TotalDisputes        int
	TotalRatings         int
	AverageScore         float64
	ScoreHistoryJSON     string
	Active               bool
	SuspendedAt          *time.Time
	UpdatedAt            time.Time
}

// AggregateResources holds summed resource counts.
type AggregateResources struct {
	TotalCPU       int
	UsedCPU        int
	TotalStorageGB int64
	UsedStorageGB  int64
	TotalMemoryGB  int64
	UsedMemoryGB   int64
}

// ---------------------------------------------------------------------------
// Tenant operations
// ---------------------------------------------------------------------------

// CreateTenant inserts a new tenant record.
func (s *Store) CreateTenant(ctx context.Context, t *TenantRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tenants (tenant_id, name, center_did, status, cpu_cores, storage_gb, memory_gb, gpu_model, last_active, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.TenantID, t.Name, t.CenterDID, t.Status, t.CPUCores, t.StorageGB, t.MemoryGB, t.GPUModel, t.LastActive, t.CreatedAt)
	return err
}

// GetTenant retrieves a tenant by ID.
func (s *Store) GetTenant(ctx context.Context, tenantID string) (*TenantRow, error) {
	var t TenantRow
	var lastActive, createdAt string
	err := s.db.QueryRowContext(ctx,
		"SELECT tenant_id, name, center_did, status, cpu_cores, storage_gb, memory_gb, gpu_model, last_active, created_at FROM tenants WHERE tenant_id = ?",
		tenantID).Scan(&t.TenantID, &t.Name, &t.CenterDID, &t.Status, &t.CPUCores, &t.StorageGB, &t.MemoryGB, &t.GPUModel, &lastActive, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.LastActive, _ = time.Parse("2006-01-02 15:04:05", lastActive)
	t.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	return &t, nil
}

// ListTenants returns all tenants.
func (s *Store) ListTenants(ctx context.Context) ([]TenantRow, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT tenant_id, name, center_did, status, cpu_cores, storage_gb, memory_gb, gpu_model, last_active, created_at FROM tenants ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tenants []TenantRow
	for rows.Next() {
		var t TenantRow
		var lastActive, createdAt string
		if err := rows.Scan(&t.TenantID, &t.Name, &t.CenterDID, &t.Status, &t.CPUCores, &t.StorageGB, &t.MemoryGB, &t.GPUModel, &lastActive, &createdAt); err != nil {
			return nil, err
		}
		t.LastActive, _ = time.Parse("2006-01-02 15:04:05", lastActive)
		t.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		tenants = append(tenants, t)
	}
	return tenants, rows.Err()
}

// UpdateTenantStatus changes a tenant's status.
func (s *Store) UpdateTenantStatus(ctx context.Context, tenantID, status string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE tenants SET status = ? WHERE tenant_id = ?", status, tenantID)
	return err
}

// CountTenants returns the total number of tenants.
func (s *Store) CountTenants(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tenants").Scan(&count)
	return count, err
}

// CountActiveTenants returns tenants active within the given duration.
func (s *Store) CountActiveTenants(ctx context.Context, since time.Duration) (int, error) {
	var count int
	cutoff := time.Now().Add(-since)
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM tenants WHERE last_active > ?", cutoff).Scan(&count)
	return count, err
}

// GetTenantsByCenter returns tenants connected to a specific center.
func (s *Store) GetTenantsByCenter(ctx context.Context, centerDID string) ([]TenantRow, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT tenant_id, name, center_did, status, cpu_cores, storage_gb, memory_gb, gpu_model, last_active, created_at FROM tenants WHERE center_did = ?",
		centerDID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tenants []TenantRow
	for rows.Next() {
		var t TenantRow
		var lastActive, createdAt string
		if err := rows.Scan(&t.TenantID, &t.Name, &t.CenterDID, &t.Status, &t.CPUCores, &t.StorageGB, &t.MemoryGB, &t.GPUModel, &lastActive, &createdAt); err != nil {
			return nil, err
		}
		t.LastActive, _ = time.Parse("2006-01-02 15:04:05", lastActive)
		t.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		tenants = append(tenants, t)
	}
	return tenants, rows.Err()
}

// GetAggregateResources sums resource counts across all active tenants.
func (s *Store) GetAggregateResources(ctx context.Context) (*AggregateResources, error) {
	var agg AggregateResources
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cpu_cores),0), COALESCE(SUM(storage_gb),0), COALESCE(SUM(memory_gb),0)
		 FROM tenants WHERE status = 'active'`).Scan(&agg.TotalCPU, &agg.TotalStorageGB, &agg.TotalMemoryGB)
	return &agg, err
}

// CountJobsByState returns the count of resource transactions in a given state.
func (s *Store) CountJobsByState(ctx context.Context, state string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM resource_transactions WHERE state = ?", state).Scan(&count)
	return count, err
}

// ---------------------------------------------------------------------------
// Central revenue operations
// ---------------------------------------------------------------------------

// CreateCentralRevenue records a revenue split.
func (s *Store) CreateCentralRevenue(ctx context.Context, r *CentralRevenueRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO central_revenue (revenue_id, transaction_id, tenant_id, total_amount, central_fee, producer_payout, processor_fee, currency, settled_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.RevenueID, r.TransactionID, r.TenantID, r.TotalAmount, r.CentralFee, r.ProducerPayout, r.ProcessorFee, r.Currency, r.SettledAt, r.CreatedAt)
	return err
}

// ---------------------------------------------------------------------------
// Auto-accept rule operations
// ---------------------------------------------------------------------------

// GetAutoAcceptRules returns all auto-accept rules ordered by priority.
func (s *Store) GetAutoAcceptRules(ctx context.Context) ([]AutoAcceptRuleRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT rule_id, rule_name, enabled, priority, min_user_score, max_amount, resource_type,
		        allowed_hours, allowed_days, require_prepay, action, notify_operator, created_at, updated_at
		 FROM auto_accept_rules ORDER BY priority ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []AutoAcceptRuleRow
	for rows.Next() {
		var r AutoAcceptRuleRow
		var enabled, requirePrepay, notifyOp int
		var createdAt, updatedAt string
		if err := rows.Scan(&r.RuleID, &r.RuleName, &enabled, &r.Priority, &r.MinUserScore, &r.MaxAmount,
			&r.ResourceType, &r.AllowedHoursJSON, &r.AllowedDaysJSON, &requirePrepay, &r.Action, &notifyOp,
			&createdAt, &updatedAt); err != nil {
			return nil, err
		}
		r.Enabled = enabled != 0
		r.RequirePrepay = requirePrepay != 0
		r.NotifyOperator = notifyOp != 0
		r.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		r.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// CreateAutoAcceptRule inserts a new auto-accept rule.
func (s *Store) CreateAutoAcceptRule(ctx context.Context, r *AutoAcceptRuleRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO auto_accept_rules
		 (rule_id, rule_name, enabled, priority, min_user_score, max_amount, resource_type,
		  allowed_hours, allowed_days, require_prepay, action, notify_operator, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.RuleID, r.RuleName, boolToInt(r.Enabled), r.Priority, r.MinUserScore, r.MaxAmount,
		r.ResourceType, r.AllowedHoursJSON, r.AllowedDaysJSON, boolToInt(r.RequirePrepay),
		r.Action, boolToInt(r.NotifyOperator), r.CreatedAt, r.UpdatedAt)
	return err
}

// ToggleAutoAcceptRule enables or disables a rule.
func (s *Store) ToggleAutoAcceptRule(ctx context.Context, ruleID string, enabled bool) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE auto_accept_rules SET enabled = ?, updated_at = ? WHERE rule_id = ?",
		boolToInt(enabled), time.Now(), ruleID)
	return err
}

// DeleteAutoAcceptRule removes a rule.
func (s *Store) DeleteAutoAcceptRule(ctx context.Context, ruleID string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM auto_accept_rules WHERE rule_id = ?", ruleID)
	return err
}

// ---------------------------------------------------------------------------
// P2P block operations
// ---------------------------------------------------------------------------

// CreateP2PBlock stores a new block produced by P2P consensus.
func (s *Store) CreateP2PBlock(ctx context.Context, b *P2PBlockRow) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO p2p_blocks (height, data, hash, prev_hash, timestamp, synced_to_central) VALUES (?, ?, ?, ?, ?, ?)",
		b.Height, b.Data, b.Hash, b.PrevHash, b.Timestamp, boolToInt(b.Synced))
	return err
}

// GetLatestBlockHeight returns the height of the most recent P2P block.
func (s *Store) GetLatestBlockHeight(ctx context.Context) (int64, error) {
	var height int64
	err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(height),0) FROM p2p_blocks").Scan(&height)
	return height, err
}

// GetLatestBlockHash returns the hash of the most recent P2P block.
func (s *Store) GetLatestBlockHash(ctx context.Context) ([]byte, error) {
	var hash []byte
	err := s.db.QueryRowContext(ctx,
		"SELECT hash FROM p2p_blocks ORDER BY height DESC LIMIT 1").Scan(&hash)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return hash, err
}

// GetUnsyncedBlocks returns all blocks not yet synced to central.
func (s *Store) GetUnsyncedBlocks(ctx context.Context) ([]P2PBlockRow, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT height, data, hash, prev_hash, timestamp FROM p2p_blocks WHERE synced_to_central = 0 ORDER BY height ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var blocks []P2PBlockRow
	for rows.Next() {
		var b P2PBlockRow
		var ts string
		if err := rows.Scan(&b.Height, &b.Data, &b.Hash, &b.PrevHash, &ts); err != nil {
			return nil, err
		}
		b.Timestamp, _ = time.Parse("2006-01-02 15:04:05", ts)
		blocks = append(blocks, b)
	}
	return blocks, rows.Err()
}

// MarkBlockSynced marks a block as synced to central.
func (s *Store) MarkBlockSynced(ctx context.Context, height int64) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE p2p_blocks SET synced_to_central = 1 WHERE height = ?", height)
	return err
}

// ---------------------------------------------------------------------------
// P2P peer operations
// ---------------------------------------------------------------------------

// GetP2PPeers returns all known peers.
func (s *Store) GetP2PPeers(ctx context.Context) ([]P2PPeerRow, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT peer_did, address, public_key, last_seen, reputation_score, cpu_cores, storage_gb, gpu_model FROM p2p_peers")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var peers []P2PPeerRow
	for rows.Next() {
		var p P2PPeerRow
		var lastSeen string
		if err := rows.Scan(&p.PeerDID, &p.Address, &p.PublicKey, &lastSeen, &p.Score, &p.CPUCores, &p.StorageGB, &p.GPUModel); err != nil {
			return nil, err
		}
		p.LastSeen, _ = time.Parse("2006-01-02 15:04:05", lastSeen)
		peers = append(peers, p)
	}
	return peers, rows.Err()
}

// UpsertP2PPeer inserts or updates a peer record.
func (s *Store) UpsertP2PPeer(ctx context.Context, p *P2PPeerRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO p2p_peers (peer_did, address, public_key, last_seen, reputation_score, cpu_cores, storage_gb, gpu_model)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(peer_did) DO UPDATE SET
		  address = excluded.address, last_seen = excluded.last_seen,
		  reputation_score = excluded.reputation_score, cpu_cores = excluded.cpu_cores,
		  storage_gb = excluded.storage_gb, gpu_model = excluded.gpu_model`,
		p.PeerDID, p.Address, p.PublicKey, p.LastSeen, p.Score, p.CPUCores, p.StorageGB, p.GPUModel)
	return err
}

// ---------------------------------------------------------------------------
// P2P pending sync operations
// ---------------------------------------------------------------------------

// GetPendingSync returns all pending sync operations.
func (s *Store) GetPendingSync(ctx context.Context) ([]P2PPendingSyncRow, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT sync_id, data_type, data, created_at, retry_count FROM p2p_pending_sync ORDER BY created_at ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ops []P2PPendingSyncRow
	for rows.Next() {
		var op P2PPendingSyncRow
		var createdAt string
		if err := rows.Scan(&op.SyncID, &op.DataType, &op.Data, &createdAt, &op.RetryCount); err != nil {
			return nil, err
		}
		op.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		ops = append(ops, op)
	}
	return ops, rows.Err()
}

// DeletePendingSync removes a sync operation.
func (s *Store) DeletePendingSync(ctx context.Context, syncID string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM p2p_pending_sync WHERE sync_id = ?", syncID)
	return err
}

// ---------------------------------------------------------------------------
// Rating alert operations
// ---------------------------------------------------------------------------

// CreateRatingAlert inserts a new rating alert.
func (s *Store) CreateRatingAlert(ctx context.Context, a *RatingAlertRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO rating_alerts
		 (alert_id, transaction_id, user_did, provider_did, center_did, alert_type, severity,
		  evidence, notes, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.AlertID, a.TransactionID, a.UserDID, a.ProviderDID, a.CenterDID,
		a.AlertType, a.Severity, a.Evidence, a.Notes, a.Status, a.CreatedAt)
	return err
}

// GetRatingAlerts returns rating alerts filtered by status.
func (s *Store) GetRatingAlerts(ctx context.Context, status string) ([]RatingAlertRow, error) {
	query := "SELECT alert_id, transaction_id, user_did, provider_did, center_did, alert_type, severity, evidence, notes, status, resolution, created_at FROM rating_alerts"
	var args []interface{}
	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var alerts []RatingAlertRow
	for rows.Next() {
		var a RatingAlertRow
		var createdAt string
		if err := rows.Scan(&a.AlertID, &a.TransactionID, &a.UserDID, &a.ProviderDID, &a.CenterDID,
			&a.AlertType, &a.Severity, &a.Evidence, &a.Notes, &a.Status, &a.Resolution, &createdAt); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

// GetOtherRating retrieves the rating from the opposite role in a transaction.
func (s *Store) GetOtherRating(ctx context.Context, transactionID string, currentRaterRole string) (*OtherRatingRow, error) {
	var oppositeRole string
	if currentRaterRole == "provider" {
		oppositeRole = "user"
	} else {
		oppositeRole = "provider"
	}
	var r OtherRatingRow
	err := s.db.QueryRowContext(ctx,
		"SELECT score FROM lbtas_ratings WHERE transaction_id = ? AND rater_role = ?",
		transactionID, oppositeRole).Scan(&r.Score)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ---------------------------------------------------------------------------
// Dispute operations
// ---------------------------------------------------------------------------

// CreateDispute inserts a new dispute.
func (s *Store) CreateDispute(ctx context.Context, d *DisputeRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO disputes (dispute_id, transaction_id, filer_did, reason, priority, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		d.DisputeID, d.TransactionID, d.FilerDID, d.Reason, d.Priority, d.Status, d.CreatedAt)
	return err
}

// GetDispute retrieves a dispute by ID.
func (s *Store) GetDispute(ctx context.Context, disputeID string) (*DisputeRow, error) {
	var d DisputeRow
	var createdAt string
	var resolvedAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		"SELECT dispute_id, transaction_id, filer_did, reason, priority, status, resolution, resolved_at, created_at FROM disputes WHERE dispute_id = ?",
		disputeID).Scan(&d.DisputeID, &d.TransactionID, &d.FilerDID, &d.Reason, &d.Priority, &d.Status, &d.Resolution, &resolvedAt, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	if resolvedAt.Valid {
		t, _ := time.Parse("2006-01-02 15:04:05", resolvedAt.String)
		d.ResolvedAt = &t
	}
	return &d, nil
}

// ResolveDispute marks a dispute as resolved.
func (s *Store) ResolveDispute(ctx context.Context, disputeID, resolution string, resolvedAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE disputes SET status = 'resolved', resolution = ?, resolved_at = ? WHERE dispute_id = ?",
		resolution, resolvedAt, disputeID)
	return err
}

// SetTransactionDispute sets the dispute reference on a resource transaction.
func (s *Store) SetTransactionDispute(ctx context.Context, transactionID, disputeID, reason string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE resource_transactions SET state = 'disputed', dispute_id = ?, dispute_reason = ?, updated_at = ? WHERE transaction_id = ?",
		disputeID, reason, time.Now(), transactionID)
	return err
}

// ---------------------------------------------------------------------------
// Investigation operations
// ---------------------------------------------------------------------------

// CreateInvestigation inserts a new investigation.
func (s *Store) CreateInvestigation(ctx context.Context, inv *InvestigationRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO investigations (investigation_id, dispute_id, status, deadline, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		inv.InvestigationID, inv.DisputeID, inv.Status, inv.Deadline, inv.CreatedAt)
	return err
}

// ---------------------------------------------------------------------------
// Center rating operations
// ---------------------------------------------------------------------------

// CreateCenterRating inserts a rating of a central SOHO.
func (s *Store) CreateCenterRating(ctx context.Context, r *CenterRatingRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO center_ratings (rating_id, dispute_id, center_did, rater_did, rater_role, score, feedback, created_at, signature)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.RatingID, r.DisputeID, r.CenterDID, r.RaterDID, r.RaterRole, r.Score, r.Feedback, r.CreatedAt, r.Signature)
	return err
}

// ---------------------------------------------------------------------------
// Center score operations
// ---------------------------------------------------------------------------

// GetCenterScore retrieves the aggregate score for a center.
func (s *Store) GetCenterScore(ctx context.Context, centerDID string) (*CenterScoreRow, error) {
	var cs CenterScoreRow
	var active int
	var suspendedAt sql.NullString
	var updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT center_did, overall_score, investigation_quality, fairness, timeliness, communication,
		        total_disputes, total_ratings, average_score, score_history, active, suspended_at, updated_at
		 FROM center_scores WHERE center_did = ?`, centerDID).Scan(
		&cs.CenterDID, &cs.OverallScore, &cs.InvestigationQuality, &cs.Fairness, &cs.Timeliness,
		&cs.Communication, &cs.TotalDisputes, &cs.TotalRatings, &cs.AverageScore,
		&cs.ScoreHistoryJSON, &active, &suspendedAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get center score: %w", err)
	}
	cs.Active = active != 0
	cs.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
	if suspendedAt.Valid {
		t, _ := time.Parse("2006-01-02 15:04:05", suspendedAt.String)
		cs.SuspendedAt = &t
	}
	return &cs, nil
}

// UpsertCenterScore inserts or updates a center's aggregate score.
func (s *Store) UpsertCenterScore(ctx context.Context, cs *CenterScoreRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO center_scores
		 (center_did, overall_score, investigation_quality, fairness, timeliness, communication,
		  total_disputes, total_ratings, average_score, score_history, active, suspended_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(center_did) DO UPDATE SET
		  overall_score = excluded.overall_score,
		  investigation_quality = excluded.investigation_quality,
		  fairness = excluded.fairness,
		  timeliness = excluded.timeliness,
		  communication = excluded.communication,
		  total_disputes = excluded.total_disputes,
		  total_ratings = excluded.total_ratings,
		  average_score = excluded.average_score,
		  score_history = excluded.score_history,
		  active = excluded.active,
		  suspended_at = excluded.suspended_at,
		  updated_at = excluded.updated_at`,
		cs.CenterDID, cs.OverallScore, cs.InvestigationQuality, cs.Fairness, cs.Timeliness,
		cs.Communication, cs.TotalDisputes, cs.TotalRatings, cs.AverageScore,
		cs.ScoreHistoryJSON, boolToInt(cs.Active), cs.SuspendedAt, cs.UpdatedAt)
	return err
}

// ---------------------------------------------------------------------------
// Dashboard revenue query operations
// ---------------------------------------------------------------------------

// RevenueRow represents a row from the central_revenue table.
type RevenueRow struct {
	RevenueID      string
	TransactionID  string
	TenantID       string
	TotalAmount    int64
	CentralFee     int64
	ProducerPayout int64
	ProcessorFee   int64
	Currency       string
	Status         string
	ResourceType   string
	CreatedAt      time.Time
	SettledAt      *time.Time
}

// GetTotalRevenue returns total revenue across all time.
func (s *Store) GetTotalRevenue(ctx context.Context) (int64, error) {
	var total sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		"SELECT COALESCE(SUM(total_amount), 0) FROM central_revenue").Scan(&total)
	if err != nil {
		return 0, err
	}
	return total.Int64, nil
}

// GetRevenueSince returns total revenue since the given time.
func (s *Store) GetRevenueSince(ctx context.Context, since time.Time) (int64, error) {
	var total sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		"SELECT COALESCE(SUM(total_amount), 0) FROM central_revenue WHERE created_at >= ?", since).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total.Int64, nil
}

// GetRevenueByType returns total revenue for a specific resource type.
func (s *Store) GetRevenueByType(ctx context.Context, resourceType string) (int64, error) {
	var total sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cr.total_amount), 0) FROM central_revenue cr
		 JOIN resource_transactions rt ON cr.transaction_id = rt.transaction_id
		 WHERE rt.resource_type = ?`, resourceType).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total.Int64, nil
}

// GetPendingPayout returns total unsettled revenue.
func (s *Store) GetPendingPayout(ctx context.Context) (int64, error) {
	var total sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		"SELECT COALESCE(SUM(producer_payout), 0) FROM central_revenue WHERE settled_at IS NULL").Scan(&total)
	if err != nil {
		return 0, err
	}
	return total.Int64, nil
}

// GetRecentRevenue returns the most recent revenue entries.
func (s *Store) GetRecentRevenue(ctx context.Context, limit int) ([]RevenueRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT cr.revenue_id, cr.transaction_id, cr.tenant_id, cr.total_amount, cr.central_fee,
		        cr.producer_payout, cr.processor_fee, cr.currency, cr.created_at,
		        COALESCE(rt.resource_type, '') as resource_type,
		        CASE WHEN cr.settled_at IS NOT NULL THEN 'settled' ELSE 'pending' END as status
		 FROM central_revenue cr
		 LEFT JOIN resource_transactions rt ON cr.transaction_id = rt.transaction_id
		 ORDER BY cr.created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []RevenueRow
	for rows.Next() {
		var r RevenueRow
		if err := rows.Scan(&r.RevenueID, &r.TransactionID, &r.TenantID, &r.TotalAmount,
			&r.CentralFee, &r.ProducerPayout, &r.ProcessorFee, &r.Currency, &r.CreatedAt,
			&r.ResourceType, &r.Status); err != nil {
			continue
		}
		result = append(result, r)
	}
	return result, nil
}

// RecordCentralRevenue inserts a RevenueRow into the central_revenue table.
// It ensures the referenced tenant exists (creating a stub if absent) then
// maps RevenueRow fields to CentralRevenueRow and delegates to CreateCentralRevenue.
func (s *Store) RecordCentralRevenue(ctx context.Context, r *RevenueRow) error {
	// Ensure the tenant exists to satisfy the FK constraint.
	if _, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO tenants (tenant_id, name) VALUES (?, '')`, r.TenantID); err != nil {
		return fmt.Errorf("failed to ensure tenant: %w", err)
	}
	row := &CentralRevenueRow{
		RevenueID:      r.RevenueID,
		TransactionID:  r.TransactionID,
		TenantID:       r.TenantID,
		TotalAmount:    r.TotalAmount,
		CentralFee:     r.CentralFee,
		ProducerPayout: r.ProducerPayout,
		ProcessorFee:   r.ProcessorFee,
		Currency:       r.Currency,
		SettledAt:      r.SettledAt,
		CreatedAt:      r.CreatedAt,
	}
	return s.CreateCentralRevenue(ctx, row)
}

// ---------------------------------------------------------------------------
// Dashboard rental query operations
// ---------------------------------------------------------------------------

// ActiveRentalRow represents an active rental transaction.
type ActiveRentalRow struct {
	TransactionID string
	UserDID       string
	ProviderDID   string
	ResourceType  string
	ResourceID    string
	PaymentAmount int64
	CreatedAt     time.Time
}

// GetActiveRentals returns all resource transactions in an active state.
func (s *Store) GetActiveRentals(ctx context.Context) ([]ActiveRentalRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT transaction_id, user_did, provider_did, resource_type, resource_id, payment_amount, created_at
		 FROM resource_transactions WHERE state IN ('initiated', 'executing')
		 ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ActiveRentalRow
	for rows.Next() {
		var r ActiveRentalRow
		if err := rows.Scan(&r.TransactionID, &r.UserDID, &r.ProviderDID, &r.ResourceType,
			&r.ResourceID, &r.PaymentAmount, &r.CreatedAt); err != nil {
			continue
		}
		result = append(result, r)
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Dashboard alert query operations
// ---------------------------------------------------------------------------

// AlertRow represents a row from the rating_alerts table.
type AlertRow struct {
	AlertID       string
	TransactionID string
	UserDID       string
	ProviderDID   string
	CenterDID     string
	AlertType     string
	Severity      string
	Status        string
	CreatedAt     time.Time
}

// ---------------------------------------------------------------------------
// Daily revenue aggregation (for mobile app charts)
// ---------------------------------------------------------------------------

// DailyRevenueRow is a per-day revenue aggregate used by the mobile dashboard.
type DailyRevenueRow struct {
	Date string // "YYYY-MM-DD"
	Sats int64
}

// GetDailyRevenueLast30Days returns per-day revenue totals for the past 30 days,
// ordered oldest-first. Days with no revenue are omitted.
func (s *Store) GetDailyRevenueLast30Days(ctx context.Context) ([]DailyRevenueRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT strftime('%Y-%m-%d', created_at) AS day,
		        COALESCE(SUM(total_amount), 0)   AS sats
		 FROM   central_revenue
		 WHERE  created_at >= datetime('now', '-30 days')
		 GROUP  BY day
		 ORDER  BY day ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []DailyRevenueRow
	for rows.Next() {
		var r DailyRevenueRow
		if err := rows.Scan(&r.Date, &r.Sats); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// GetPayoutsByStatus returns pending/processing payouts across all providers,
// used by the PayoutSettler to retry failed dispatches.
func (s *Store) GetPayoutsByStatus(ctx context.Context, status string, limit int) ([]PayoutRow, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT payout_id, provider_did, amount_sats, processor,
		       status, external_id, error_msg, requested_at, settled_at
		  FROM payouts
		 WHERE status = ?
		 ORDER BY requested_at ASC LIMIT ?`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PayoutRow
	for rows.Next() {
		var p PayoutRow
		var settledAt sql.NullTime
		if err := rows.Scan(
			&p.PayoutID, &p.ProviderDID, &p.AmountSats, &p.Processor,
			&p.Status, &p.ExternalID, &p.ErrorMsg, &p.RequestedAt, &settledAt,
		); err != nil {
			return nil, fmt.Errorf("scan payout row: %w", err)
		}
		if settledAt.Valid {
			t := settledAt.Time
			p.SettledAt = &t
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// GetRecentAlerts returns the most recent rating alerts.
func (s *Store) GetRecentAlerts(ctx context.Context, limit int) ([]AlertRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT alert_id, transaction_id, user_did, provider_did, center_did, alert_type, severity, status, created_at
		 FROM rating_alerts ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []AlertRow
	for rows.Next() {
		var a AlertRow
		if err := rows.Scan(&a.AlertID, &a.TransactionID, &a.UserDID, &a.ProviderDID,
			&a.CenterDID, &a.AlertType, &a.Severity, &a.Status, &a.CreatedAt); err != nil {
			continue
		}
		result = append(result, a)
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Payout operations (Gap 2 — real payout records backed by the payouts table)
// ---------------------------------------------------------------------------

// PayoutRow represents a payout request in the payouts table.
type PayoutRow struct {
	PayoutID    string
	ProviderDID string
	AmountSats  int64
	Processor   string     // "lightning", "stripe", "barter", etc.
	Status      string     // "pending", "processing", "settled", "failed"
	ExternalID  string     // processor-side transaction/payment ID
	ErrorMsg    string
	RequestedAt time.Time
	SettledAt   *time.Time
}

// CreatePayout inserts a new payout record. The caller must set PayoutID.
func (s *Store) CreatePayout(ctx context.Context, p *PayoutRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO payouts
			(payout_id, provider_did, amount_sats, processor, status, external_id, error_msg, requested_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.PayoutID, p.ProviderDID, p.AmountSats, p.Processor,
		p.Status, p.ExternalID, p.ErrorMsg, p.RequestedAt)
	return err
}

// UpdatePayoutStatus advances the status of a payout and optionally records
// the processor-side ID and any error message.
func (s *Store) UpdatePayoutStatus(ctx context.Context, payoutID, status, externalID, errMsg string) error {
	var settledAt interface{}
	if status == "settled" {
		settledAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE payouts
		   SET status=?, external_id=?, error_msg=?, settled_at=?
		 WHERE payout_id=?`,
		status, externalID, errMsg, settledAt, payoutID)
	return err
}

// ListPayouts returns recent payout records for a provider, newest first.
// Pass limit ≤ 0 for a default of 50.
func (s *Store) ListPayouts(ctx context.Context, providerDID string, limit int) ([]PayoutRow, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT payout_id, provider_did, amount_sats, processor,
		       status, external_id, error_msg, requested_at, settled_at
		  FROM payouts
		 WHERE provider_did = ?
		 ORDER BY requested_at DESC LIMIT ?`, providerDID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PayoutRow
	for rows.Next() {
		var p PayoutRow
		var settledAt sql.NullTime
		if err := rows.Scan(
			&p.PayoutID, &p.ProviderDID, &p.AmountSats, &p.Processor,
			&p.Status, &p.ExternalID, &p.ErrorMsg, &p.RequestedAt, &settledAt,
		); err != nil {
			return nil, fmt.Errorf("scan payout row: %w", err)
		}
		if settledAt.Valid {
			t := settledAt.Time
			p.SettledAt = &t
		}
		result = append(result, p)
	}
	return result, rows.Err()
}
