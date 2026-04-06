package adaptive

import (
	"testing"
	"time"
)

func TestEstimatorConvergesAndReturnsRecommendation(t *testing.T) {
	e := NewRTTEstimator()
	samples := []time.Duration{
		100 * time.Millisecond,
		110 * time.Millisecond,
		95 * time.Millisecond,
		105 * time.Millisecond,
		100 * time.Millisecond,
	}
	for _, s := range samples {
		e.Update(s)
	}

	if e.NSamples() != len(samples) {
		t.Fatalf("unexpected sample count: got %d want %d", e.NSamples(), len(samples))
	}
	if e.SRTT() <= 0 {
		t.Fatalf("SRTT should be > 0, got %v", e.SRTT())
	}

	rec := e.Recommend()
	if rec.KATimeout < 1*time.Second || rec.KATimeout > 30*time.Second {
		t.Fatalf("KATimeout out of bounds: %v", rec.KATimeout)
	}
	if rec.KATime < 1*time.Second || rec.KATime > 60*time.Second {
		t.Fatalf("KATime out of bounds: %v", rec.KATime)
	}
	if rec.KATime <= rec.KATimeout {
		t.Fatalf("KATime should exceed KATimeout: time=%v timeout=%v", rec.KATime, rec.KATimeout)
	}
}

func TestInitialRTOWhenNoSamples(t *testing.T) {
	e := NewRTTEstimator()
	if got := e.RTO(); got != initialRTO {
		t.Fatalf("unexpected initial RTO: got %v want %v", got, initialRTO)
	}
}
