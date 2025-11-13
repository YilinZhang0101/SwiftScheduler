package main

import (
	"context"
	"io"
	"log"
	"os"
	"time"

	pb "github.com/YilinZhang0101/SwiftScheduler/proto" // module path
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	masterAddr := "localhost:50051" // master's address and port
	log.Printf("Attempting to connect to master at %s", masterAddr)

	// 1. [Connect] Create a connection to the gRPC server
	// We use insecure.NewCredentials() to skip TLS (local development)
	conn, err := grpc.Dial(masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to master: %v", err)
	}
	defer conn.Close()

	// 2. [Create client]
	client := pb.NewSchedulerServiceClient(conn)

	// 3. [Call Connect] Open a bidirectional stream
	// Use context.Background() since the stream should persist
	stream, err := client.Connect(context.Background())
	if err != nil {
		log.Fatalf("Failed to open stream: %v", err)
	}

	// 4. [Send registration] First send RegisterRequest after connection
	workerID, _ := os.Hostname() // use hostname as workerID
	req := &pb.WorkerMessage{
		WorkerId: workerID,
		Payload: &pb.WorkerMessage_RegisterRequest{
			RegisterRequest: &pb.RegisterRequest{
				Hostname:      workerID,
				MaxConcurrency: 10, // temporarily hardcoded value
			},
		},
	}

	if err := stream.Send(req); err != nil {
		log.Fatalf("Failed to send register request: %v", err)
	}

	log.Printf("Worker %s successfully sent register request.", workerID)

	// 5. [Start receiving goroutine]
	// Continuously receive messages from the master in a separate goroutine
	go func() {
		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				// Master closed the connection
				log.Println("Master closed the connection.")
				return
			}
			if err != nil {
				log.Printf("Error receiving message from master: %v", err)
				return
			}

			// --- Placeholder for Phase 1 core logic ---
			switch x := msg.Payload.(type) {
			case *pb.MasterMessage_RegisterResponse:
				log.Printf("Successfully registered! Message from master: %s", x.RegisterResponse.Message)
			case *pb.MasterMessage_TaskAssignment:
				log.Printf("Received new task: %s", x.TaskAssignment.TaskId)
				// TODO: Step 1: Execute the task
				// TODO: Step 2: Send StatusUpdate (report task completion)
			default:
				log.Printf("Received unknown message type from master")
			}
			// ---
		}
	}()

	// 6. [Keep main goroutine alive]
	// The main goroutine also needs to do work, like sending heartbeats
	// In Phase 1, simulate StatusUpdate as heartbeat/load report with a ticker
	ticker := time.NewTicker(10 * time.Second) // every 10 seconds
	defer ticker.Stop()

	for range ticker.C {
		log.Println("Sending periodic status update...")
		// TODO: Step 3: Send StatusUpdate (including active_task_count)
		// updateMsg := &pb.WorkerMessage{ ... }
		// if err := stream.Send(updateMsg); err != nil {
		//   log.Printf("Failed to send status update: %v", err)
		// }
	}
}