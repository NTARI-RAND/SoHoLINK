package central

import (
	"sort"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// pricing.go tests
// ---------------------------------------------------------------------------

func TestCalculateFees_BasicExample(t *testing.T) {
	// $10.00 payment with 2.9% processor fee
	fee := CalculateFees(1000, 0.029)
	if fee.UserPaid != 1000 {
		t.Errorf("UserPaid = %d, want 1000", fee.UserPaid)
	}
	// processor: int64(1000 * 0.029) = 29
	if fee.ProcessorFee != 29 {
		t.Errorf("ProcessorFee = %d, want 29", fee.ProcessorFee)
	}
	// net: 1000 - 29 = 971
	if fee.NetAmount != 971 {
		t.Errorf("NetAmount = %d, want 971", fee.NetAmount)
	}
	// central: 971 / 100 = 9
	if fee.CentralFee != 9 {
		t.Errorf("CentralFee = %d, want 9", fee.CentralFee)
	}
	// producer: 971 - 9 = 962
	if fee.ProducerPayout != 962 {
		t.Errorf("ProducerPayout = %d, want 962", fee.ProducerPayout)
	}
	if fee.Currency != "USD" {
		t.Errorf("Currency = %q, want USD", fee.Currency)
	}
}

func TestCalculateFees_TableDriven(t *testing.T) {
	tests := []struct {
		name           string
		userPayment    int64
		processorPct   float64
		wantProcessor  int64
		wantNet        int64
		wantCentral    int64
		wantProducer   int64
	}{
		{
			name:          "zero_payment",
			userPayment:   0,
			processorPct:  0.029,
			wantProcessor: 0,
			wantNet:       0,
			wantCentral:   0,
			wantProducer:  0,
		},
		{
			name:          "small_payment_1_cent",
			userPayment:   1,
			processorPct:  0.029,
			wantProcessor: 0, // int64(0.029) = 0
			wantNet:       1,
			wantCentral:   0, // 1/100 = 0
			wantProducer:  1,
		},
		{
			name:          "100_dollar_payment",
			userPayment:   10000,
			processorPct:  0.029,
			wantProcessor: 290,
			wantNet:       9710,
			wantCentral:   97,
			wantProducer:  9613,
		},
		{
			name:          "zero_processor_fee",
			userPayment:   5000,
			processorPct:  0.0,
			wantProcessor: 0,
			wantNet:       5000,
			wantCentral:   50,
			wantProducer:  4950,
		},
		{
			name:          "large_payment_10000_dollars",
			userPayment:   1000000,
			processorPct:  0.029,
			wantProcessor: 29000,
			wantNet:       971000,
			wantCentral:   9710,
			wantProducer:  961290,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fee := CalculateFees(tt.userPayment, tt.processorPct)
			if fee.ProcessorFee != tt.wantProcessor {
				t.Errorf("ProcessorFee = %d, want %d", fee.ProcessorFee, tt.wantProcessor)
			}
			if fee.NetAmount != tt.wantNet {
				t.Errorf("NetAmount = %d, want %d", fee.NetAmount, tt.wantNet)
			}
			if fee.CentralFee != tt.wantCentral {
				t.Errorf("CentralFee = %d, want %d", fee.CentralFee, tt.wantCentral)
			}
			if fee.ProducerPayout != tt.wantProducer {
				t.Errorf("ProducerPayout = %d, want %d", fee.ProducerPayout, tt.wantProducer)
			}
			if fee.TotalAmount != tt.userPayment {
				t.Errorf("TotalAmount = %d, want %d", fee.TotalAmount, tt.userPayment)
			}
		})
	}
}

func TestCalculateFees_SumsCorrectly(t *testing.T) {
	// Invariant: CentralFee + ProducerPayout == NetAmount
	payments := []int64{1, 50, 99, 100, 999, 1000, 5000, 10000, 99999}
	for _, p := range payments {
		fee := CalculateFees(p, 0.029)
		if fee.CentralFee+fee.ProducerPayout != fee.NetAmount {
			t.Errorf("payment=%d: CentralFee(%d) + ProducerPayout(%d) != NetAmount(%d)",
				p, fee.CentralFee, fee.ProducerPayout, fee.NetAmount)
		}
		if fee.ProcessorFee+fee.NetAmount != fee.UserPaid {
			t.Errorf("payment=%d: ProcessorFee(%d) + NetAmount(%d) != UserPaid(%d)",
				p, fee.ProcessorFee, fee.NetAmount, fee.UserPaid)
		}
	}
}

func TestCalculateFees_TimestampSet(t *testing.T) {
	before := time.Now()
	fee := CalculateFees(1000, 0.029)
	after := time.Now()
	if fee.CreatedAt.Before(before) || fee.CreatedAt.After(after) {
		t.Error("CreatedAt should be between before and after")
	}
}

func TestCalculateFeesWithFixed_BasicStripeExample(t *testing.T) {
	// $10.00, Stripe 2.9% + 30 cents
	fee := CalculateFeesWithFixed(1000, 0.029, 30)
	// processor: int64(1000 * 0.029) + 30 = 29 + 30 = 59
	if fee.ProcessorFee != 59 {
		t.Errorf("ProcessorFee = %d, want 59", fee.ProcessorFee)
	}
	// net: 1000 - 59 = 941
	if fee.NetAmount != 941 {
		t.Errorf("NetAmount = %d, want 941", fee.NetAmount)
	}
	// central: 941 / 100 = 9
	if fee.CentralFee != 9 {
		t.Errorf("CentralFee = %d, want 9", fee.CentralFee)
	}
	// producer: 941 - 9 = 932
	if fee.ProducerPayout != 932 {
		t.Errorf("ProducerPayout = %d, want 932", fee.ProducerPayout)
	}
}

func TestCalculateFeesWithFixed_TableDriven(t *testing.T) {
	tests := []struct {
		name          string
		userPayment   int64
		percentFee    float64
		fixedFee      int64
		wantProcessor int64
		wantNet       int64
		wantCentral   int64
		wantProducer  int64
	}{
		{
			name:          "zero_payment",
			userPayment:   0,
			percentFee:    0.029,
			fixedFee:      30,
			wantProcessor: 0, // capped at userPayment
			wantNet:       0,
			wantCentral:   0,
			wantProducer:  0,
		},
		{
			name:          "processor_fee_exceeds_payment",
			userPayment:   10,
			percentFee:    0.029,
			fixedFee:      30,
			wantProcessor: 10, // capped at 10 (int64(0.29)+30=30 > 10)
			wantNet:       0,
			wantCentral:   0,
			wantProducer:  0,
		},
		{
			name:          "no_fixed_fee",
			userPayment:   5000,
			percentFee:    0.029,
			fixedFee:      0,
			wantProcessor: 145, // int64(5000*0.029) = 145
			wantNet:       4855,
			wantCentral:   48,
			wantProducer:  4807,
		},
		{
			name:          "no_percent_fee",
			userPayment:   5000,
			percentFee:    0.0,
			fixedFee:      30,
			wantProcessor: 30,
			wantNet:       4970,
			wantCentral:   49,
			wantProducer:  4921,
		},
		{
			name:          "large_payment",
			userPayment:   1000000,
			percentFee:    0.029,
			fixedFee:      30,
			wantProcessor: 29030,
			wantNet:       970970,
			wantCentral:   9709,
			wantProducer:  961261,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fee := CalculateFeesWithFixed(tt.userPayment, tt.percentFee, tt.fixedFee)
			if fee.ProcessorFee != tt.wantProcessor {
				t.Errorf("ProcessorFee = %d, want %d", fee.ProcessorFee, tt.wantProcessor)
			}
			if fee.NetAmount != tt.wantNet {
				t.Errorf("NetAmount = %d, want %d", fee.NetAmount, tt.wantNet)
			}
			if fee.CentralFee != tt.wantCentral {
				t.Errorf("CentralFee = %d, want %d", fee.CentralFee, tt.wantCentral)
			}
			if fee.ProducerPayout != tt.wantProducer {
				t.Errorf("ProducerPayout = %d, want %d", fee.ProducerPayout, tt.wantProducer)
			}
		})
	}
}

func TestCalculateFeesWithFixed_ProcessorFeeCapAtPayment(t *testing.T) {
	// If processor fee > userPayment, it should be capped at userPayment
	fee := CalculateFeesWithFixed(5, 0.5, 100)
	if fee.ProcessorFee != 5 {
		t.Errorf("ProcessorFee = %d, want 5 (capped at userPayment)", fee.ProcessorFee)
	}
	if fee.NetAmount != 0 {
		t.Errorf("NetAmount = %d, want 0", fee.NetAmount)
	}
}

func TestCalculateFeesWithFixed_SumsCorrectly(t *testing.T) {
	payments := []int64{1, 50, 100, 500, 1000, 10000, 50000}
	for _, p := range payments {
		fee := CalculateFeesWithFixed(p, 0.029, 30)
		if fee.CentralFee+fee.ProducerPayout != fee.NetAmount {
			t.Errorf("payment=%d: CentralFee(%d) + ProducerPayout(%d) != NetAmount(%d)",
				p, fee.CentralFee, fee.ProducerPayout, fee.NetAmount)
		}
		if fee.ProcessorFee+fee.NetAmount != fee.UserPaid {
			t.Errorf("payment=%d: ProcessorFee(%d) + NetAmount(%d) != UserPaid(%d)",
				p, fee.ProcessorFee, fee.NetAmount, fee.UserPaid)
		}
	}
}

// ---------------------------------------------------------------------------
// notifier.go tests
// ---------------------------------------------------------------------------

func TestNewNotifier(t *testing.T) {
	n := NewNotifier()
	if n == nil {
		t.Fatal("NewNotifier() returned nil")
	}
}

func TestNotifier_Subscribe(t *testing.T) {
	n := NewNotifier()
	ch := n.Subscribe()
	if ch == nil {
		t.Fatal("Subscribe() returned nil channel")
	}
}

func TestNotifier_SendReceive(t *testing.T) {
	n := NewNotifier()
	ch := n.Subscribe()

	notif := Notification{
		Type:     "test_event",
		Severity: "info",
		Title:    "Test",
		Message:  "Hello world",
	}
	n.Send(notif)

	select {
	case got := <-ch:
		if got.Type != "test_event" {
			t.Errorf("Type = %q, want test_event", got.Type)
		}
		if got.Message != "Hello world" {
			t.Errorf("Message = %q, want Hello world", got.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for notification")
	}
}

func TestNotifier_DefaultSeverity(t *testing.T) {
	n := NewNotifier()
	ch := n.Subscribe()

	// Send with empty severity; should be set to "info"
	n.Send(Notification{Type: "test", Message: "msg"})

	select {
	case got := <-ch:
		if got.Severity != "info" {
			t.Errorf("Severity = %q, want info", got.Severity)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestNotifier_ExplicitSeverityPreserved(t *testing.T) {
	n := NewNotifier()
	ch := n.Subscribe()

	n.Send(Notification{Type: "test", Severity: "critical", Message: "bad"})

	select {
	case got := <-ch:
		if got.Severity != "critical" {
			t.Errorf("Severity = %q, want critical", got.Severity)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestNotifier_MultipleSubscribers(t *testing.T) {
	n := NewNotifier()
	ch1 := n.Subscribe()
	ch2 := n.Subscribe()
	ch3 := n.Subscribe()

	n.Send(Notification{Type: "broadcast", Message: "msg"})

	for i, ch := range []<-chan Notification{ch1, ch2, ch3} {
		select {
		case got := <-ch:
			if got.Type != "broadcast" {
				t.Errorf("subscriber %d: Type = %q, want broadcast", i, got.Type)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out", i)
		}
	}
}

func TestNotifier_ConcurrentSendSafe(t *testing.T) {
	n := NewNotifier()
	_ = n.Subscribe()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			n.Send(Notification{Type: "concurrent", Message: "test"})
		}(i)
	}
	wg.Wait()
	// If we reach here without a race detector panic, the test passes.
}

func TestNotifier_SubscribeConcurrent(t *testing.T) {
	n := NewNotifier()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := n.Subscribe()
			if ch == nil {
				t.Error("Subscribe() returned nil")
			}
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// capacity.go tests (struct + alert logic)
// ---------------------------------------------------------------------------

func TestCapacityMetrics_ZeroValues(t *testing.T) {
	var m CapacityMetrics
	if m.TotalCPUCores != 0 || m.UsedCPUCores != 0 {
		t.Error("zero-value CapacityMetrics should have 0 CPU cores")
	}
	if m.CPUUtilization != 0.0 {
		t.Error("zero-value CPUUtilization should be 0")
	}
}

func TestCapacityAlert_Fields(t *testing.T) {
	alert := CapacityAlert{
		Severity: "critical",
		Resource: "cpu",
		Message:  "CPU at 95%",
	}
	if alert.Severity != "critical" {
		t.Errorf("Severity = %q, want critical", alert.Severity)
	}
	if alert.Resource != "cpu" {
		t.Errorf("Resource = %q, want cpu", alert.Resource)
	}
}

func TestCapacityMonitor_DefaultThresholds(t *testing.T) {
	// NewCapacityMonitor requires a store, but we can pass nil for unit testing
	// the alert threshold defaults since checkAlerts doesn't touch the store.
	mon := NewCapacityMonitor(nil, time.Minute)
	if mon == nil {
		t.Fatal("NewCapacityMonitor returned nil")
	}
	// Default thresholds are 0.80
	if mon.cpuAlertThreshold != 0.80 {
		t.Errorf("cpuAlertThreshold = %f, want 0.80", mon.cpuAlertThreshold)
	}
	if mon.storageAlertThreshold != 0.80 {
		t.Errorf("storageAlertThreshold = %f, want 0.80", mon.storageAlertThreshold)
	}
}

func TestCapacityMonitor_AlertChan(t *testing.T) {
	mon := NewCapacityMonitor(nil, time.Minute)
	ch := mon.AlertChan()
	if ch == nil {
		t.Fatal("AlertChan() returned nil")
	}
}

func TestCapacityMonitor_GetLatestMetricsNilInitially(t *testing.T) {
	mon := NewCapacityMonitor(nil, time.Minute)
	if mon.GetLatestMetrics() != nil {
		t.Error("GetLatestMetrics() should be nil before Run()")
	}
}

func TestCapacityMonitor_CheckAlerts_CPUAboveThreshold(t *testing.T) {
	mon := NewCapacityMonitor(nil, time.Minute)

	metrics := CapacityMetrics{
		TotalCPUCores:  100,
		UsedCPUCores:   90,
		CPUUtilization: 0.90, // above 0.80 threshold
	}
	mon.checkAlerts(metrics)

	select {
	case alert := <-mon.alertChan:
		if alert.Resource != "cpu" {
			t.Errorf("Resource = %q, want cpu", alert.Resource)
		}
		if alert.Severity != "warning" {
			t.Errorf("Severity = %q, want warning", alert.Severity)
		}
	case <-time.After(time.Second):
		t.Fatal("expected CPU alert, got nothing")
	}
}

func TestCapacityMonitor_CheckAlerts_CPUBelowThreshold(t *testing.T) {
	mon := NewCapacityMonitor(nil, time.Minute)

	metrics := CapacityMetrics{
		TotalCPUCores:  100,
		UsedCPUCores:   50,
		CPUUtilization: 0.50,
	}
	mon.checkAlerts(metrics)

	select {
	case alert := <-mon.alertChan:
		t.Errorf("unexpected alert: %+v", alert)
	default:
		// No alert expected — pass
	}
}

func TestCapacityMonitor_CheckAlerts_StorageAboveThreshold(t *testing.T) {
	mon := NewCapacityMonitor(nil, time.Minute)

	metrics := CapacityMetrics{
		TotalStorageGB: 1000,
		UsedStorageGB:  900, // 90% > 80% threshold
	}
	mon.checkAlerts(metrics)

	select {
	case alert := <-mon.alertChan:
		if alert.Resource != "storage" {
			t.Errorf("Resource = %q, want storage", alert.Resource)
		}
	case <-time.After(time.Second):
		t.Fatal("expected storage alert, got nothing")
	}
}

func TestCapacityMonitor_CheckAlerts_StorageBelowThreshold(t *testing.T) {
	mon := NewCapacityMonitor(nil, time.Minute)

	metrics := CapacityMetrics{
		TotalStorageGB: 1000,
		UsedStorageGB:  500,
	}
	mon.checkAlerts(metrics)

	select {
	case alert := <-mon.alertChan:
		t.Errorf("unexpected alert: %+v", alert)
	default:
		// pass
	}
}

func TestCapacityMonitor_CheckAlerts_StorageExhaustionProjection(t *testing.T) {
	mon := NewCapacityMonitor(nil, time.Minute)

	// DaysUntilStorageFull < 30 should trigger alert
	metrics := CapacityMetrics{
		DaysUntilStorageFull: 15,
	}
	mon.checkAlerts(metrics)

	select {
	case alert := <-mon.alertChan:
		if alert.Resource != "storage" {
			t.Errorf("Resource = %q, want storage", alert.Resource)
		}
	case <-time.After(time.Second):
		t.Fatal("expected storage exhaustion alert, got nothing")
	}
}

func TestCapacityMonitor_CheckAlerts_StorageExhaustionFarOff(t *testing.T) {
	mon := NewCapacityMonitor(nil, time.Minute)

	// DaysUntilStorageFull >= 30 should NOT trigger alert
	metrics := CapacityMetrics{
		DaysUntilStorageFull: 60,
	}
	mon.checkAlerts(metrics)

	select {
	case alert := <-mon.alertChan:
		t.Errorf("unexpected alert for far-off storage: %+v", alert)
	default:
		// pass
	}
}

func TestCapacityMonitor_CheckAlerts_MultipleAlerts(t *testing.T) {
	mon := NewCapacityMonitor(nil, time.Minute)

	metrics := CapacityMetrics{
		TotalCPUCores:        100,
		UsedCPUCores:         95,
		CPUUtilization:       0.95,
		TotalStorageGB:       1000,
		UsedStorageGB:        950,
		DaysUntilStorageFull: 10,
	}
	mon.checkAlerts(metrics)

	// Should get 3 alerts: cpu warning, storage warning, storage exhaustion
	alerts := make([]CapacityAlert, 0, 3)
	for i := 0; i < 3; i++ {
		select {
		case a := <-mon.alertChan:
			alerts = append(alerts, a)
		case <-time.After(time.Second):
			break
		}
	}

	if len(alerts) != 3 {
		t.Fatalf("expected 3 alerts, got %d", len(alerts))
	}

	// Sort by resource for deterministic checking
	sort.Slice(alerts, func(i, j int) bool {
		return alerts[i].Resource < alerts[j].Resource
	})
	if alerts[0].Resource != "cpu" {
		t.Errorf("first alert resource = %q, want cpu", alerts[0].Resource)
	}
}

// ---------------------------------------------------------------------------
// TransactionFee / DisputeRequest / Resolution struct tests
// ---------------------------------------------------------------------------

func TestTransactionFee_FieldAssignment(t *testing.T) {
	now := time.Now()
	fee := TransactionFee{
		TransactionID:  "tx_123",
		TotalAmount:    1000,
		CentralFee:     10,
		ProducerPayout: 931,
		UserPaid:       1000,
		ProcessorFee:   59,
		NetAmount:      941,
		Currency:       "USD",
		CreatedAt:      now,
	}
	if fee.TransactionID != "tx_123" {
		t.Errorf("TransactionID = %q, want tx_123", fee.TransactionID)
	}
}

func TestDisputeRequest_Fields(t *testing.T) {
	req := DisputeRequest{
		TransactionID: "tx_1",
		FilerDID:      "did:soho:user1",
		Reason:        "not delivered",
		Priority:      "high",
	}
	if req.Priority != "high" {
		t.Errorf("Priority = %q, want high", req.Priority)
	}
}

func TestResolution_Actions(t *testing.T) {
	actions := []string{"refund_user", "payout_provider", "split_50_50", "no_action"}
	for _, a := range actions {
		r := Resolution{Action: a, Explanation: "test"}
		if r.Action != a {
			t.Errorf("Action = %q, want %q", r.Action, a)
		}
	}
}
