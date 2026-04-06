package scheduler

import (
	"testing"
	"time"

	pb "github.com/YilinZhang0101/SwiftScheduler/proto"
)

func TestSelectWorkerLeastLoad(t *testing.T) {
	sm := NewStateManager()
	sm.workers["w1"] = &WorkerStats{ID: "w1", MaxConcurrency: 4, ActiveTaskCount: 2}
	sm.workers["w2"] = &WorkerStats{ID: "w2", MaxConcurrency: 4, ActiveTaskCount: 1}
	sm.workers["w3"] = &WorkerStats{ID: "w3", MaxConcurrency: 4, ActiveTaskCount: 3}

	got, err := sm.SelectWorker()
	if err != nil {
		t.Fatalf("SelectWorker returned error: %v", err)
	}
	if got != "w2" {
		t.Fatalf("unexpected worker selected: got %s want w2", got)
	}
}

func TestCheckHeartbeatTimeoutDedup(t *testing.T) {
	sm := NewStateManager()
	sm.workers["w1"] = &WorkerStats{
		ID:       "w1",
		LastSeen: time.Now().Add(-10 * time.Second),
	}

	first := sm.CheckHeartbeatTimeout(2 * time.Second)
	if len(first) != 1 || first[0] != "w1" {
		t.Fatalf("expected first timeout to include w1, got %+v", first)
	}

	second := sm.CheckHeartbeatTimeout(2 * time.Second)
	if len(second) != 0 {
		t.Fatalf("expected dedup timeout report, got %+v", second)
	}
}

func TestUpdateWorkerStatusClearsSuspected(t *testing.T) {
	sm := NewStateManager()
	sm.workers["w1"] = &WorkerStats{
		ID:        "w1",
		Suspected: true,
		LastSeen:  time.Now().Add(-10 * time.Second),
	}

	sm.UpdateWorkerStatus("w1", &pb.StatusUpdate{ActiveTaskCount: 3})

	ws := sm.workers["w1"]
	if ws.Suspected {
		t.Fatalf("expected Suspected=false after status update")
	}
	if ws.ActiveTaskCount != 3 {
		t.Fatalf("unexpected ActiveTaskCount: got %d want 3", ws.ActiveTaskCount)
	}
}
