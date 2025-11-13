package main

import (
	"context"
	"fmt"
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
	hostname, _ := os.Hostname() // use hostname as workerID
	// add PID to prevent multiple instances of the same worker on the same machine
	workerID := fmt.Sprintf("%s-%d", hostname, os.Getpid())
	
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
	}()    // parameter for goroutine

	// 6. [Keep main goroutine alive]
	// The main goroutine also needs to do work, like sending heartbeats
	// In Phase 1, simulate StatusUpdate as heartbeat/load report with a ticker
	ticker := time.NewTicker(5 * time.Second) // every 5 seconds
	defer ticker.Stop()

	for range ticker.C {
		// TODO: Step 3: Send StatusUpdate (including active_task_count)
		// set current load
		currentActiveTasks := int32(0)

		log.Printf("Sending heartbeat... (Active Tasks: %d)", currentActiveTasks)

        // build StatusUpdate message
        updateMsg := &pb.WorkerMessage{
            WorkerId: workerID, // reuse the workerID from the beginning
            Payload: &pb.WorkerMessage_StatusUpdate{
                StatusUpdate: &pb.StatusUpdate{
                    ActiveTaskCount: currentActiveTasks,
                    // TODO: extend CPU/Memory usage here
                },
            },
        }

        // send message
        if err := stream.Send(updateMsg); err != nil {
            // if sending fails, it usually means the connection is broken
            log.Printf("Failed to send status update: %v", err)
            // in production, this usually triggers the "reconnect logic" (Reconnect)
            // for now, we can just break or return to exit the program
            break 
        }
	}
}