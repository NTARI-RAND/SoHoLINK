package store

import (
	"context"
	"errors"
	"testing"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/metrics"
	dto "github.com/prometheus/client_model/go"
)

func uptimeDuration(t *testing.T) *dto.Histogram {
	t.Helper()
	var observation dto.Metric
	if err := metrics.UptimeScorerDuration.Write(&observation); err != nil {
		t.Fatal(err)
	}
	return observation.GetHistogram()
}

func TestScoreUptimeRecordsDurationForSuccessAndFailure(t *testing.T) {
	db, _ := uptimeTestDB(t)
	before := uptimeDuration(t)
	if err := scoreUptime(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	after := uptimeDuration(t)
	if got := after.GetSampleCount(); got != before.GetSampleCount()+1 {
		t.Errorf("successful run observations = %d; want %d", got, before.GetSampleCount()+1)
	}
	if after.GetSampleSum() <= before.GetSampleSum() {
		t.Error("successful run did not record positive elapsed seconds")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := scoreUptime(cancelled, db); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled run error = %v; want context.Canceled", err)
	}
	if got := uptimeDuration(t).GetSampleCount(); got != before.GetSampleCount()+2 {
		t.Errorf("all run observations = %d; want %d", got, before.GetSampleCount()+2)
	}
}
