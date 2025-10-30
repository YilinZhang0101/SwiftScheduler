package scheduler

import (
	"log"
	"sync" // sync for mutexes

	pb "github.com/YilinZhang0101/SwiftScheduler/proto" // module path
)

// WorkerStats stores the Master's knowledge about a Worker.
// This is the data foundation for our "global view".
type WorkerStats struct {
	ID             string
	Hostname       string
	MaxConcurrency int32
	// --- Core load metric for Phase 1 ---
	ActiveTaskCount int32
	// TODO: In Phase 2, expand to a richer health score
}

// StateManager manages all workers' state.
// It is a thread-safe component.
type StateManager struct {
	// RWMutex allows concurrent readers and exclusive writers
	mu      sync.RWMutex
	workers map[string]*WorkerStats // key is worker_id
}

// NewStateManager constructs a StateManager
func NewStateManager() *StateManager {
	return &StateManager{
		workers: make(map[string]*WorkerStats),
	}
}

// RegisterWorker is called when a worker connects
func (sm *StateManager) RegisterWorker(req *pb.RegisterRequest, workerID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stats := &WorkerStats{
		ID:              workerID,
		Hostname:        req.Hostname,
		MaxConcurrency:  req.MaxConcurrency,
		ActiveTaskCount: 0, // newly registered worker starts with 0 active tasks
	}
	sm.workers[workerID] = stats

	log.Printf("[StateManager] Worker %s registered. Total workers: %d", workerID, len(sm.workers))
}

// UnregisterWorker is called when a worker disconnects
func (sm *StateManager) UnregisterWorker(workerID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.workers, workerID)
	log.Printf("[StateManager] Worker %s unregistered. Total workers: %d", workerID, len(sm.workers))
}

// UpdateWorkerStatus updates a worker's status based on a StatusUpdate
func (sm *StateManager) UpdateWorkerStatus(workerID string, update *pb.StatusUpdate) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if ws, ok := sm.workers[workerID]; ok {
		ws.ActiveTaskCount = update.ActiveTaskCount
		return
	}
	log.Printf("[StateManager] Received status for unknown worker: %s", workerID)
}

// GetGlobalLoad returns (sumActive, sumCapacity)
func (sm *StateManager) GetGlobalLoad() (int32, int32) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var sumActive int32
	var sumCapacity int32
	for _, ws := range sm.workers {
		sumActive += ws.ActiveTaskCount
		sumCapacity += ws.MaxConcurrency
	}
	return sumActive, sumCapacity
}