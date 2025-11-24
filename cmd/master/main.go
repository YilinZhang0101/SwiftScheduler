package main

import (
	// "context"
	"io"
	"fmt"
	"log"
	"net"

	pb "github.com/YilinZhang0101/SwiftScheduler/proto" // module path
	"github.com/YilinZhang0101/SwiftScheduler/internal/scheduler"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer" // used to get client information
)

// Upgrade masterServer to hold a pointer to the StateManager
type masterServer struct {
	pb.UnimplementedSchedulerServiceServer
	stateManager *scheduler.StateManager // dependency injection
}

// [Core Logic] Implement Connect
func (s *masterServer) Connect(stream pb.SchedulerService_ConnectServer) error {
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
			log.Printf("Worker %s disconnected (EOF).", workerID)
			return nil // exit loop; defer will run
		}
		if err != nil {
			// Connection closed abnormally
			log.Printf("Connection error with worker %s: %v", workerID, err)
			return err // exit loop; defer will run
		}

		// --- Phase 1 core logic placeholder ---
		switch payload := msg.Payload.(type) {
		case *pb.WorkerMessage_StatusUpdate:
			log.Printf("Received StatusUpdate from %s: ActiveTasks=%d", msg.WorkerId, payload.StatusUpdate.ActiveTaskCount)
			// TODO: call s.stateManager.UpdateWorkerStatus(...)
			s.stateManager.UpdateWorkerStatus(msg.WorkerId, payload.StatusUpdate)
		default:
			log.Printf("Received unknown message type from %s", msg.WorkerId)
		}
		// ---
	}
}

// main function: program entrypoint
func main() {
	port := ":50051"
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// [Important] Create a single instance of StateManager
	sm := scheduler.NewStateManager()

	s := grpc.NewServer()

	// [Important] Inject the StateManager instance into masterServer
	pb.RegisterSchedulerServiceServer(s, &masterServer{
		stateManager: sm,
	})

	log.Printf("Master server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}