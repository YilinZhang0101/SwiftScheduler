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

    // --- Placeholder for Phase 1 core logic ---
    // TODO: Step 1: Receive the Worker's RegisterRequest
    // TODO: Step 2: Add the Worker to the State Manager
    // TODO: Step 3: Start a goroutine to receive the Worker's StatusUpdate (heartbeat and load)
    // TODO: Step 4: Push TaskAssignment to the Worker via the stream (not implemented yet)
	// ---

    // Keep the connection open until the client disconnects or an error occurs
    // We need a loop to receive Worker messages; this will be implemented later
    // For now, block with a simple select {} to prevent the method from returning
	select {}

    // When the connection is closed (e.g., Worker exits)
    // log.Printf("Worker disconnected: %s", clientAddr) // This line will not execute for now
    // TODO: Step 5: Remove the Worker from the State Manager
    // return nil // This line will not execute for now
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
	pb.RegisterSchedulerServiceServer(s, &masterServer{})

	log.Printf("Master server listening at %v", lis.Addr())

    // Start the server and begin accepting connections
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}