// Package adaptive implements keepalive parameter tuning based on RFC 6298.
//
// The core insight: gRPC keepalive timeout is the same problem as TCP's
// retransmission timeout (RTO) — "how long to wait before declaring the
// other side dead?" TCP solved this in 1988 with the Jacobson/Karels
// algorithm. We apply the same formula to keepalive tuning.
//
// Reference: https://www.rfc-editor.org/rfc/rfc6298
package adaptive

import (
	"math"
	"time"
)

const (
	// RFC 6298 recommended constants
	alpha = 0.125 // 1/8 — SRTT smoothing factor
	beta  = 0.25  // 1/4 — RTTVAR smoothing factor
	K     = 4.0   // multiplier for variance term

	// Safety bounds — keepalive params should stay within sane limits
	minKATime    = 1 * time.Second
	maxKATime    = 60 * time.Second
	minKATimeout = 1 * time.Second
	maxKATimeout = 30 * time.Second

	// RFC 6298: initial RTO before any samples = 1s
	initialRTO = 1 * time.Second
)

// RTTEstimator tracks smoothed RTT and variance using the Jacobson/Karels algorithm.
// It is NOT thread-safe — callers should synchronize if used concurrently.
type RTTEstimator struct {
	srtt   float64 // smoothed RTT in seconds
	rttvar float64 // RTT variance in seconds
	nSamples int   // number of samples observed so far
}

// NewRTTEstimator creates a fresh estimator with no prior samples.
func NewRTTEstimator() *RTTEstimator {
	return &RTTEstimator{}
}

// Update ingests a new RTT sample and updates the internal estimates.
// Call this each time a keepalive PING round-trip is measured.
func (e *RTTEstimator) Update(sample time.Duration) {
	r := sample.Seconds()

	if e.nSamples == 0 {
		// RFC 6298 §2.2: first measurement — initialize directly
		e.srtt = r
		e.rttvar = r / 2.0
	} else {
		// RFC 6298 §2.3: subsequent measurements
		// RTTVAR = (1 - beta) * RTTVAR + beta * |SRTT - R|
		e.rttvar = (1-beta)*e.rttvar + beta*math.Abs(e.srtt-r)
		// SRTT = (1 - alpha) * SRTT + alpha * R
		e.srtt = (1-alpha)*e.srtt + alpha*r
	}
	e.nSamples++
}

// RTO returns the recommended retransmission timeout (= keepalive_timeout).
// Formula: RTO = SRTT + K * RTTVAR  (RFC 6298 §2.4)
func (e *RTTEstimator) RTO() time.Duration {
	if e.nSamples == 0 {
		return initialRTO
	}
	rto := e.srtt + K*e.rttvar
	return clamp(time.Duration(rto*float64(time.Second)), minKATimeout, maxKATimeout)
}

// SRTT returns the current smoothed RTT estimate.
func (e *RTTEstimator) SRTT() time.Duration {
	return time.Duration(e.srtt * float64(time.Second))
}

// RTTVAR returns the current RTT variance estimate.
func (e *RTTEstimator) RTTVAR() time.Duration {
	return time.Duration(e.rttvar * float64(time.Second))
}

// NSamples returns how many RTT samples have been observed.
func (e *RTTEstimator) NSamples() int {
	return e.nSamples
}

// KeepaliveRecommendation holds the suggested gRPC keepalive parameters.
type KeepaliveRecommendation struct {
	// KATime is the suggested keepalive_time (interval between PINGs).
	// We set this to 3× SRTT — frequent enough to detect failures quickly,
	// but not so aggressive that we flood the network with PINGs.
	KATime time.Duration

	// KATimeout is the suggested keepalive_timeout (how long to wait for
	// a PING response before declaring the connection dead).
	// Directly derived from RFC 6298 RTO formula.
	KATimeout time.Duration

	// Jitter is the current RTT standard deviation — a measure of network
	// instability. High jitter → conservative params to avoid false positives.
	Jitter time.Duration

	// Confidence indicates how reliable this recommendation is.
	// Low confidence means we haven't seen enough samples yet.
	Confidence string
}

// Recommend computes suggested keepalive parameters based on current RTT estimates.
func (e *RTTEstimator) Recommend() KeepaliveRecommendation {
	timeout := e.RTO()

	// KATime: send PINGs at 3× SRTT interval.
	// Rationale: gives enough headroom for a round-trip before the next PING,
	// while still being responsive. Too small → PING storms. Too large → slow detection.
	kaTime := clamp(3*e.SRTT(), minKATime, maxKATime)

	// Ensure KATime > KATimeout (otherwise we'd declare failure before the next PING)
	if kaTime <= timeout {
		kaTime = timeout + 1*time.Second
	}

	confidence := "high"
	if e.nSamples < 5 {
		confidence = "low (need more samples)"
	} else if e.nSamples < 20 {
		confidence = "medium"
	}

	return KeepaliveRecommendation{
		KATime:     kaTime,
		KATimeout:  timeout,
		Jitter:     e.RTTVAR(),
		Confidence: confidence,
	}
}

// clamp constrains d to [lo, hi].
func clamp(d, lo, hi time.Duration) time.Duration {
	if d < lo {
		return lo
	}
	if d > hi {
		return hi
	}
	return d
}
