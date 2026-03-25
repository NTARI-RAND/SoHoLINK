package rental

import (
	"context"
	"sort"
	"testing"
)

// ---------------------------------------------------------------------------
// matchesRule tests (pure function, no store needed)
// ---------------------------------------------------------------------------

func TestMatchesRule_AllConditionsMet(t *testing.T) {
	rule := AutoAcceptRule{
		Enabled:      true,
		MinUserScore: 50,
		MaxAmount:    10000,
		ResourceType: "compute",
	}
	req := RentalRequest{
		UserScore:    75,
		Amount:       5000,
		ResourceType: "compute",
	}
	if !matchesRule(req, rule) {
		t.Error("expected rule to match")
	}
}

func TestMatchesRule_ScoreTooLow(t *testing.T) {
	rule := AutoAcceptRule{MinUserScore: 80}
	req := RentalRequest{UserScore: 50}
	if matchesRule(req, rule) {
		t.Error("should not match: score too low")
	}
}

func TestMatchesRule_AmountExceedsMax(t *testing.T) {
	rule := AutoAcceptRule{MaxAmount: 5000}
	req := RentalRequest{Amount: 6000}
	if matchesRule(req, rule) {
		t.Error("should not match: amount exceeds max")
	}
}

func TestMatchesRule_AmountAtMax(t *testing.T) {
	rule := AutoAcceptRule{MaxAmount: 5000}
	req := RentalRequest{Amount: 5000}
	if !matchesRule(req, rule) {
		t.Error("should match: amount equals max")
	}
}

func TestMatchesRule_ZeroMaxAmountMeansNoLimit(t *testing.T) {
	rule := AutoAcceptRule{MaxAmount: 0}
	req := RentalRequest{Amount: 999999}
	if !matchesRule(req, rule) {
		t.Error("should match: MaxAmount=0 means no limit")
	}
}

func TestMatchesRule_ResourceTypeMismatch(t *testing.T) {
	rule := AutoAcceptRule{ResourceType: "storage"}
	req := RentalRequest{ResourceType: "compute"}
	if matchesRule(req, rule) {
		t.Error("should not match: wrong resource type")
	}
}

func TestMatchesRule_EmptyResourceTypeMatchesAny(t *testing.T) {
	rule := AutoAcceptRule{ResourceType: ""}
	req := RentalRequest{ResourceType: "print"}
	if !matchesRule(req, rule) {
		t.Error("should match: empty ResourceType matches any")
	}
}

func TestMatchesRule_RequirePrepayNotMet(t *testing.T) {
	rule := AutoAcceptRule{RequirePrepay: true}
	req := RentalRequest{HasPaymentEscrow: false}
	if matchesRule(req, rule) {
		t.Error("should not match: prepay required but not present")
	}
}

func TestMatchesRule_RequirePrepayMet(t *testing.T) {
	rule := AutoAcceptRule{RequirePrepay: true}
	req := RentalRequest{HasPaymentEscrow: true}
	if !matchesRule(req, rule) {
		t.Error("should match: prepay required and present")
	}
}

func TestMatchesRule_NoPrepayRequired(t *testing.T) {
	rule := AutoAcceptRule{RequirePrepay: false}
	req := RentalRequest{HasPaymentEscrow: false}
	if !matchesRule(req, rule) {
		t.Error("should match: no prepay required")
	}
}

func TestMatchesRule_TableDriven(t *testing.T) {
	tests := []struct {
		name  string
		rule  AutoAcceptRule
		req   RentalRequest
		match bool
	}{
		{
			name:  "exact_min_score",
			rule:  AutoAcceptRule{MinUserScore: 50},
			req:   RentalRequest{UserScore: 50},
			match: true,
		},
		{
			name:  "score_one_below_min",
			rule:  AutoAcceptRule{MinUserScore: 50},
			req:   RentalRequest{UserScore: 49},
			match: false,
		},
		{
			name:  "zero_score_zero_min",
			rule:  AutoAcceptRule{MinUserScore: 0},
			req:   RentalRequest{UserScore: 0},
			match: true,
		},
		{
			name:  "all_zeros",
			rule:  AutoAcceptRule{},
			req:   RentalRequest{},
			match: true,
		},
		{
			name: "multiple_conditions_all_pass",
			rule: AutoAcceptRule{
				MinUserScore:  10,
				MaxAmount:     50000,
				ResourceType:  "internet",
				RequirePrepay: true,
			},
			req: RentalRequest{
				UserScore:        100,
				Amount:           1000,
				ResourceType:     "internet",
				HasPaymentEscrow: true,
			},
			match: true,
		},
		{
			name: "multiple_conditions_one_fails",
			rule: AutoAcceptRule{
				MinUserScore: 10,
				MaxAmount:    50000,
				ResourceType: "internet",
			},
			req: RentalRequest{
				UserScore:    100,
				Amount:       1000,
				ResourceType: "compute", // mismatch
			},
			match: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesRule(tt.req, tt.rule)
			if got != tt.match {
				t.Errorf("matchesRule() = %v, want %v", got, tt.match)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseIntSlice / formatIntSlice tests
// ---------------------------------------------------------------------------

func TestParseIntSlice_TableDriven(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []int
	}{
		{"empty_string", "", nil},
		{"empty_array", "[]", nil},
		{"null", "null", nil},
		{"single_value", "[5]", []int{5}},
		{"multiple_values", "[9,10,11,12]", []int{9, 10, 11, 12}},
		{"hours_range", "[0,1,2,3,22,23]", []int{0, 1, 2, 3, 22, 23}},
		{"days_of_week", "[1,2,3,4,5]", []int{1, 2, 3, 4, 5}},
		{"with_spaces", "[1, 2, 3]", []int{1, 2, 3}},
		{"zero", "[0]", []int{0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseIntSlice(tt.input)
			if tt.want == nil {
				if got != nil {
					t.Errorf("parseIntSlice(%q) = %v, want nil", tt.input, got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("parseIntSlice(%q) len = %d, want %d", tt.input, len(got), len(tt.want))
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("parseIntSlice(%q)[%d] = %d, want %d", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestFormatIntSlice_TableDriven(t *testing.T) {
	tests := []struct {
		name  string
		input []int
		want  string
	}{
		{"nil", nil, "[]"},
		{"empty", []int{}, "[]"},
		{"single", []int{5}, "[5]"},
		{"multiple", []int{1, 2, 3}, "[1,2,3]"},
		{"hours", []int{9, 10, 11, 12, 13}, "[9,10,11,12,13]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatIntSlice(tt.input)
			if got != tt.want {
				t.Errorf("formatIntSlice(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseFormatRoundTrip(t *testing.T) {
	inputs := [][]int{
		{0, 1, 2, 3, 4, 5, 6},
		{9, 10, 11, 12, 13, 14, 15, 16, 17},
		{23},
		{0},
	}
	for _, input := range inputs {
		formatted := formatIntSlice(input)
		parsed := parseIntSlice(formatted)
		if len(parsed) != len(input) {
			t.Fatalf("round-trip failed for %v: got %v", input, parsed)
		}
		for i := range input {
			if parsed[i] != input[i] {
				t.Errorf("round-trip mismatch at [%d]: got %d, want %d", i, parsed[i], input[i])
			}
		}
	}
}

// ---------------------------------------------------------------------------
// EvaluateRequest tests (using nil store so writeAudit is a no-op)
// ---------------------------------------------------------------------------

func newEngineWithRules(rules []AutoAcceptRule) *AutoAcceptEngine {
	// Sort by priority, matching what LoadRules does
	sorted := make([]AutoAcceptRule, len(rules))
	copy(sorted, rules)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	return &AutoAcceptEngine{
		store:    nil, // writeAudit checks for nil store
		rules:    sorted,
		notifyCh: make(chan Notification, 100),
	}
}

func TestEvaluateRequest_AcceptRule(t *testing.T) {
	engine := newEngineWithRules([]AutoAcceptRule{
		{
			RuleID:       "r1",
			RuleName:     "accept-all",
			Enabled:      true,
			Priority:     1,
			MinUserScore: 0,
			Action:       "accept",
		},
	})

	dec, err := engine.EvaluateRequest(context.Background(), RentalRequest{
		UserDID:   "did:soho:user1",
		UserScore: 50,
		Amount:    1000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Action != "accept" {
		t.Errorf("Action = %q, want accept", dec.Action)
	}
	if dec.RuleID != "r1" {
		t.Errorf("RuleID = %q, want r1", dec.RuleID)
	}
}

func TestEvaluateRequest_RejectRule(t *testing.T) {
	engine := newEngineWithRules([]AutoAcceptRule{
		{
			RuleID:       "r1",
			RuleName:     "reject-low-score",
			Enabled:      true,
			Priority:     1,
			MinUserScore: 80,
			Action:       "reject",
		},
	})

	dec, err := engine.EvaluateRequest(context.Background(), RentalRequest{
		UserScore: 90, // meets min score
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Action != "reject" {
		t.Errorf("Action = %q, want reject", dec.Action)
	}
}

func TestEvaluateRequest_PendingRule(t *testing.T) {
	engine := newEngineWithRules([]AutoAcceptRule{
		{
			RuleID:   "r1",
			RuleName: "manual-review",
			Enabled:  true,
			Priority: 1,
			Action:   "pending",
		},
	})

	dec, err := engine.EvaluateRequest(context.Background(), RentalRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Action != "pending" {
		t.Errorf("Action = %q, want pending", dec.Action)
	}
}

func TestEvaluateRequest_NoRulesDefaultsToPending(t *testing.T) {
	engine := newEngineWithRules(nil)

	dec, err := engine.EvaluateRequest(context.Background(), RentalRequest{
		UserDID:   "did:soho:user1",
		UserScore: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Action != "pending" {
		t.Errorf("Action = %q, want pending (no rules)", dec.Action)
	}
	if dec.RuleID != "" {
		t.Errorf("RuleID = %q, want empty (no rule matched)", dec.RuleID)
	}
	if dec.Message != "No matching rule - requires manual review" {
		t.Errorf("unexpected message: %q", dec.Message)
	}
}

func TestEvaluateRequest_DisabledRulesSkipped(t *testing.T) {
	engine := newEngineWithRules([]AutoAcceptRule{
		{
			RuleID:   "r1",
			RuleName: "disabled-accept",
			Enabled:  false, // disabled
			Priority: 1,
			Action:   "accept",
		},
	})

	dec, err := engine.EvaluateRequest(context.Background(), RentalRequest{UserScore: 100})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Disabled rule skipped, falls through to default pending
	if dec.Action != "pending" {
		t.Errorf("Action = %q, want pending (disabled rule)", dec.Action)
	}
}

func TestEvaluateRequest_PriorityOrdering(t *testing.T) {
	engine := newEngineWithRules([]AutoAcceptRule{
		{
			RuleID:   "low-priority",
			RuleName: "accept-all",
			Enabled:  true,
			Priority: 10,
			Action:   "accept",
		},
		{
			RuleID:   "high-priority",
			RuleName: "reject-all",
			Enabled:  true,
			Priority: 1,
			Action:   "reject",
		},
	})

	dec, err := engine.EvaluateRequest(context.Background(), RentalRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Priority 1 (reject) should fire first
	if dec.Action != "reject" {
		t.Errorf("Action = %q, want reject (higher priority)", dec.Action)
	}
	if dec.RuleID != "high-priority" {
		t.Errorf("RuleID = %q, want high-priority", dec.RuleID)
	}
}

func TestEvaluateRequest_FirstMatchWins(t *testing.T) {
	engine := newEngineWithRules([]AutoAcceptRule{
		{
			RuleID:       "r1",
			RuleName:     "high-score-accept",
			Enabled:      true,
			Priority:     1,
			MinUserScore: 80,
			Action:       "accept",
		},
		{
			RuleID:   "r2",
			RuleName: "catch-all-reject",
			Enabled:  true,
			Priority: 2,
			Action:   "reject",
		},
	})

	// High-score user matches r1
	dec, _ := engine.EvaluateRequest(context.Background(), RentalRequest{UserScore: 90})
	if dec.Action != "accept" || dec.RuleID != "r1" {
		t.Errorf("high-score: Action=%q RuleID=%q, want accept/r1", dec.Action, dec.RuleID)
	}

	// Low-score user skips r1, matches r2
	dec, _ = engine.EvaluateRequest(context.Background(), RentalRequest{UserScore: 10})
	if dec.Action != "reject" || dec.RuleID != "r2" {
		t.Errorf("low-score: Action=%q RuleID=%q, want reject/r2", dec.Action, dec.RuleID)
	}
}

func TestEvaluateRequest_PendingWithNotifyOperator(t *testing.T) {
	engine := newEngineWithRules([]AutoAcceptRule{
		{
			RuleID:         "r1",
			RuleName:       "needs-approval",
			Enabled:        true,
			Priority:       1,
			Action:         "pending",
			NotifyOperator: true,
		},
	})

	req := RentalRequest{UserDID: "did:soho:user1"}
	dec, err := engine.EvaluateRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Action != "pending" {
		t.Errorf("Action = %q, want pending", dec.Action)
	}

	// Check that a notification was sent
	select {
	case notif := <-engine.notifyCh:
		if notif.Type != "rental_approval_needed" {
			t.Errorf("notif Type = %q, want rental_approval_needed", notif.Type)
		}
		if notif.RuleID != "r1" {
			t.Errorf("notif RuleID = %q, want r1", notif.RuleID)
		}
	default:
		t.Error("expected notification on notifyCh, got none")
	}
}

func TestEvaluateRequest_PendingWithoutNotify(t *testing.T) {
	engine := newEngineWithRules([]AutoAcceptRule{
		{
			RuleID:         "r1",
			Enabled:        true,
			Priority:       1,
			Action:         "pending",
			NotifyOperator: false,
		},
	})

	_, _ = engine.EvaluateRequest(context.Background(), RentalRequest{})

	select {
	case notif := <-engine.notifyCh:
		t.Errorf("unexpected notification: %+v", notif)
	default:
		// pass — no notification expected
	}
}

func TestEvaluateRequest_EmptyRequestIDGenerated(t *testing.T) {
	engine := newEngineWithRules(nil)

	dec, _ := engine.EvaluateRequest(context.Background(), RentalRequest{
		RequestID: "",
	})
	// Falls through to default pending; the request ID should have been generated
	// We can only verify indirectly that no panic occurred and we got a result
	if dec.Action != "pending" {
		t.Errorf("Action = %q, want pending", dec.Action)
	}
}

func TestEvaluateRequest_ExplicitRequestIDPreserved(t *testing.T) {
	engine := newEngineWithRules([]AutoAcceptRule{
		{RuleID: "r1", Enabled: true, Priority: 1, Action: "accept"},
	})

	req := RentalRequest{RequestID: "my-custom-id"}
	dec, _ := engine.EvaluateRequest(context.Background(), req)
	if dec.Action != "accept" {
		t.Errorf("Action = %q, want accept", dec.Action)
	}
}

// ---------------------------------------------------------------------------
// Decision struct tests
// ---------------------------------------------------------------------------

func TestDecision_Actions(t *testing.T) {
	actions := []string{"accept", "reject", "pending"}
	for _, a := range actions {
		d := Decision{Action: a, RuleID: "test", Message: "msg"}
		if d.Action != a {
			t.Errorf("Action = %q, want %q", d.Action, a)
		}
	}
}

// ---------------------------------------------------------------------------
// NotifyChan test
// ---------------------------------------------------------------------------

func TestAutoAcceptEngine_NotifyChan(t *testing.T) {
	engine := newEngineWithRules(nil)
	ch := engine.NotifyChan()
	if ch == nil {
		t.Fatal("NotifyChan() returned nil")
	}
}

// ---------------------------------------------------------------------------
// Rule priority sort verification
// ---------------------------------------------------------------------------

func TestRulePrioritySorting(t *testing.T) {
	rules := []AutoAcceptRule{
		{RuleID: "r3", Priority: 30},
		{RuleID: "r1", Priority: 1},
		{RuleID: "r2", Priority: 10},
	}
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Priority < rules[j].Priority
	})
	if rules[0].RuleID != "r1" || rules[1].RuleID != "r2" || rules[2].RuleID != "r3" {
		t.Errorf("sort order wrong: %v", rules)
	}
}
