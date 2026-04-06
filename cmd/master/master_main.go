package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"
	"runtime"

	pb "github.com/YilinZhang0101/SwiftScheduler/proto"
	"github.com/YilinZhang0101/SwiftScheduler/internal/scheduler"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"
)

func parseDurationEnv(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Printf("invalid duration for %s=%q, using default %v", key, v, def)
		return def
	}
	return d
}

func parseIntEnv(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	var x int
	_, err := fmt.Sscanf(v, "%d", &x)
	if err != nil || x <= 0 {
		log.Printf("invalid int for %s=%q, using default %d", key, v, def)
		return def
	}
	return x
}

// Upgrade masterServer to hold a pointer to the StateManager
type masterServer struct {
	pb.UnimplementedSchedulerServiceServer
	stateManager *scheduler.StateManager
}

// [Core Logic] Implement Connect
func (s *masterServer) Connect(stream pb.SchedulerService_ConnectServer) error {
	connectStart := time.Now()
	// 1. Get client information
	p, ok := peer.FromContext(stream.Context())
	if !ok {
		return fmt.Errorf("failed to get peer from context")
	}
	clientAddr := p.Addr.String()
	log.Printf("Worker connected from: %s", clientAddr)

	// 2. [Critical] Receive the first message; it must be RegisterRequest
	firstMsg, err := stream.Recv()
	if err != nil {
		log.Printf("Failed to receive first message from %s: %v", clientAddr, err)
		return err
	}

	var workerID string
	// Use type assertion to verify the payload is a RegisterRequest
	if req, ok := firstMsg.Payload.(*pb.WorkerMessage_RegisterRequest); ok {
		workerID = firstMsg.WorkerId
		// 3. [Register] Add the Worker to the StateManager, pass the stream
		s.stateManager.RegisterWorker(req.RegisterRequest, workerID, stream)
	} else {
		// If the first message is not a registration, reject the connection
		log.Printf("Worker from %s sent invalid first message. Disconnecting.", clientAddr)
		return fmt.Errorf("first message must be a RegisterRequest")
	}

	// 4. [Critical] Ensure the worker is unregistered on disconnect using defer
	// Defer executes even if Connect returns due to normal exit, error, or panic
	defer s.stateManager.UnregisterWorker(workerID)

	// 5. [Respond] Inform the Worker that registration succeeded
	resp := &pb.MasterMessage{
		Payload: &pb.MasterMessage_RegisterResponse{
			RegisterResponse: &pb.RegisterResponse{
				Success: true,
				Message: "Successfully registered with master.",
			},
		},
	}
	if err := stream.Send(resp); err != nil {
		log.Printf("Failed to send register response to %s: %v", workerID, err)
		return err
	}

	// 6. [Main loop] Keep the connection and continuously receive heartbeats/status
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			// Connection closed normally
			detectAt := time.Now()
			log.Printf("[Detect] side=master worker=%s time=%s type=EOF elapsed=%v",
				workerID,
				detectAt.UTC().Format(time.RFC3339Nano),
				detectAt.Sub(connectStart),
			)
			return nil
		}
		if err != nil {
			// Connection closed abnormally
			detectAt := time.Now()
			log.Printf("[Detect] side=master worker=%s time=%s type=RECV_ERR err=%v elapsed=%v",
				workerID,
				detectAt.UTC().Format(time.RFC3339Nano),
				err,
				detectAt.Sub(connectStart),
			)
			return err
		}

		// Handle incoming messages from workers
		switch payload := msg.Payload.(type) {
		case *pb.WorkerMessage_StatusUpdate:
			log.Printf("Received StatusUpdate from %s: ActiveTasks=%d", msg.WorkerId, payload.StatusUpdate.ActiveTaskCount)
			s.stateManager.UpdateWorkerStatus(msg.WorkerId, payload.StatusUpdate)
		default:
			log.Printf("Received unknown message type from %s", msg.WorkerId)
		}
	}
}

// main function: program entrypoint
func main() {
	addr := os.Getenv("MASTER_LISTEN_ADDR")
	if addr == "" {
		addr = ":50051"
	}

	// Set keepalive parameters
	kaTime := parseDurationEnv("MASTER_KA_TIME", 10*time.Second)
	kaTimeout := parseDurationEnv("MASTER_KA_TIMEOUT", 5*time.Second)

	// Configure keepalive
	kaParams := keepalive.ServerParameters{
		Time:    kaTime,    // keepalive_time
		Timeout: kaTimeout, // keepalive_timeout
	}

	// Limitation to worker
	// MinTime should be less than or equal to the client's keepalive Time
	// For aggressive configs (e.g., 2s/1s), we need to allow faster pings
	kaEnforcement := keepalive.EnforcementPolicy{
		MinTime:             1 * time.Second, // allow client ping freq as low as 1s
		PermitWithoutStream: true,            // ping without active RPC
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// [Important] Create a single instance of StateManager
	sm := scheduler.NewStateManager()

	s := grpc.NewServer(
		grpc.KeepaliveParams(kaParams),
		grpc.KeepaliveEnforcementPolicy(kaEnforcement),
	)

	// [Important] Inject the StateManager instance into masterServer
	pb.RegisterSchedulerServiceServer(s, &masterServer{
		stateManager: sm,
	})

	// Start a simple task generator for testing (optional)
	if os.Getenv("MASTER_ENABLE_TASKGEN") == "1" {
		go startSimpleTaskGenerator(sm)
	}

	// observe status every 5 seconds
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for range t.C {
			sumActive, sumCap := sm.GetGlobalLoad()
			log.Printf("[Stats] time=%s workers=%d goroutines=%d sum_active=%d sum_capacity=%d",
				time.Now().UTC().Format(time.RFC3339Nano),
				sm.WorkerCount(),
				runtime.NumGoroutine(),
				sumActive,
				sumCap,
			)
		}
	}()

	// HB timeout monitor
	hbTimeout := parseDurationEnv("MASTER_HB_TIMEOUT", 6*time.Second)
	hbScanEvery := parseDurationEnv("MASTER_HB_SCAN", 200*time.Millisecond)
	go func() {
		ticker := time.NewTicker(hbScanEvery)
		defer ticker.Stop()
		for range ticker.C {
			ids := sm.CheckHeartbeatTimeout(hbTimeout)
			for _, wid := range ids {
				detectAt := time.Now()
				log.Printf("[Detect] side=master worker=%s time=%s type=HB_TIMEOUT hb_timeout=%v",
					wid,
					detectAt.UTC().Format(time.RFC3339Nano),
					hbTimeout,
				)
			}
		}
	}()

	log.Printf("Master server listening at %v", lis.Addr())
	log.Printf("[Master] Keepalive config: Time=%v, Timeout=%v, MinTime=%v", kaTime, kaTimeout, 1*time.Second)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

// startSimpleTaskGenerator: A simple task generator for testing
// This generates test tasks every 10 seconds and assigns them to available workers
func startSimpleTaskGenerator(sm *scheduler.StateManager) {
	tick := parseDurationEnv("MASTER_TASK_TICK", 10*time.Millisecond)
	batch := parseIntEnv("MASTER_TASK_BATCH", 50)

	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	taskCounter := 0
	log.Printf("[TaskGenerator] Started. tick=%v batch=%d", tick, batch)

	for range ticker.C {
		for i := 0; i < batch; i++ {
			taskCounter++
			taskID := fmt.Sprintf("task-%d", taskCounter)
			taskName := fmt.Sprintf("TestTask-%d", taskCounter)
			taskPayload := []byte(fmt.Sprintf("Test task #%d - generated at %s", taskCounter, time.Now().Format(time.RFC3339Nano)))

			workerID, err := sm.SelectWorker()
			if err != nil {
				// no available workers right now
				break
			}

			assignment := &pb.MasterMessage{
				Payload: &pb.MasterMessage_TaskAssignment{
					TaskAssignment: &pb.TaskAssignment{
						TaskId:      taskID,
						TaskName:    taskName,
						TaskPayload: taskPayload,
					},
				},
			}

			if err := sm.SendTask(workerID, assignment); err != nil {
				log.Printf("[TaskGenerator] Send failed task=%s worker=%s err=%v", taskID, workerID, err)
			}
		}

		// optional: periodic global load log
		sumActive, sumCap := sm.GetGlobalLoad()
		log.Printf("[GlobalLoad] active=%d capacity=%d workers=%d", sumActive, sumCap, sm.WorkerCount())
	}
}