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

// Define the Master server struct
// It must implement the SchedulerServiceServer interface defined in the proto file
type masterServer struct {
	pb.UnimplementedSchedulerServiceServer // embed for forward compatibility; good practice
	state *scheduler.StateManager
}

// Implement the Connect method (core of the gRPC service)
// This method is called when a Worker connects
func (s *masterServer) Connect(stream pb.SchedulerService_ConnectServer) error {
	// 1. Get client information (for logging)
	p, ok := peer.FromContext(stream.Context())
	if !ok {
		log.Println("Failed to get peer from context")
		return fmt.Errorf("failed to get peer from context")
	}
	clientAddr := p.Addr.String()
	log.Printf("Worker connected from: %s", clientAddr)

	// --- Phase 1 minimal executable protocol handling ---
	// Expect first message to be RegisterRequest, then acknowledge and start receiving updates.
	firstMsg, err := stream.Recv()
	if err == io.EOF {
		log.Printf("Worker at %s disconnected before registering", clientAddr)
		return nil
	}
	if err != nil {
		log.Printf("Error receiving first message from %s: %v", clientAddr, err)
		return err
	}

	workerID := firstMsg.GetWorkerId()
	if reg := firstMsg.GetRegisterRequest(); reg != nil {
		// Register worker in state manager
		s.state.RegisterWorker(reg, workerID)
		// Send RegisterResponse back
		resp := &pb.MasterMessage{
			Payload: &pb.MasterMessage_RegisterResponse{RegisterResponse: &pb.RegisterResponse{
				Success: true,
				Message: "Registered",
			}},
		}
		if err := stream.Send(resp); err != nil {
			log.Printf("Failed to send RegisterResponse to %s: %v", clientAddr, err)
			return err
		}
		log.Printf("Worker %s registered (host=%s, max=%d)", workerID, reg.Hostname, reg.MaxConcurrency)
	} else {
		log.Printf("First message from %s missing RegisterRequest", clientAddr)
		return fmt.Errorf("protocol error: expected RegisterRequest")
	}

	// Receive loop for subsequent messages (StatusUpdate, etc.)
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			log.Printf("Worker %s disconnected", workerID)
			s.state.UnregisterWorker(workerID)
			return nil
		}
		if err != nil {
			log.Printf("Error receiving from worker %s: %v", workerID, err)
			s.state.UnregisterWorker(workerID)
			return err
		}

		switch x := msg.Payload.(type) {
		case *pb.WorkerMessage_StatusUpdate:
			s.state.UpdateWorkerStatus(workerID, x.StatusUpdate)
			active, capacity := s.state.GetGlobalLoad()
			log.Printf("[Status] worker=%s active=%d global=(%d/%d)", workerID, x.StatusUpdate.ActiveTaskCount, active, capacity)
		case *pb.WorkerMessage_RegisterRequest:
			// Ignore duplicate registrations on the same stream
			log.Printf("Worker %s sent duplicate RegisterRequest; ignoring", workerID)
		default:
			log.Printf("Worker %s sent unknown payload type", workerID)
		}
	}
}

// main function: program entrypoint
func main() {
	port := ":50051" // port the Master listens on
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// Create a new gRPC server instance
	s := grpc.NewServer()

	// Register our masterServer (implements the service interface) with the gRPC server
	sm := scheduler.NewStateManager()
	pb.RegisterSchedulerServiceServer(s, &masterServer{state: sm})

	log.Printf("Master server listening at %v", lis.Addr())

	// Start the server and begin accepting connections
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}