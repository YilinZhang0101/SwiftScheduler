package main

import (
	"context"
	"io"
	"log"
	"os"
	"time"

	pb "github.com/YilinZhang0101/SwiftScheduler/proto" // 模块路径!
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	masterAddr := "localhost:50051" // Master 的地址和端口
	log.Printf("Attempting to connect to master at %s", masterAddr)

	// 1. [连接] 创建一个到 gRPC 服务器的连接
	// 我们使用 insecure.NewCredentials() 来跳过 TLS (因为是本地开发)
	conn, err := grpc.Dial(masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to master: %v", err)
	}
	defer conn.Close()

	// 2. [创建客户端]
	client := pb.NewSchedulerServiceClient(conn)

	// 3. [调用 Connect] 调用 Connect 方法，开启双向流
	// 我们使用 context.Background()，因为这个流应该一直持续
	stream, err := client.Connect(context.Background())
	if err != nil {
		log.Fatalf("Failed to open stream: %v", err)
	}

	// 4. [发送注册] 连接成功后，第一件事就是发送 RegisterRequest
	workerID, _ := os.Hostname() // 使用主机名作为 workerID
	req := &pb.WorkerMessage{
		WorkerId: workerID,
		Payload: &pb.WorkerMessage_RegisterRequest{
			RegisterRequest: &pb.RegisterRequest{
				Hostname:      workerID,
				MaxConcurrency: 10, // 暂时硬编码一个值
			},
		},
	}

	if err := stream.Send(req); err != nil {
		log.Fatalf("Failed to send register request: %v", err)
	}

	log.Printf("Worker %s successfully sent register request.", workerID)

	// 5. [启动接收 Goroutine] 
	// 我们必须在一个单独的 goroutine 中持续接收来自 Master 的消息
	go func() {
		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				// Master 关闭了连接
				log.Println("Master closed the connection.")
				return
			}
			if err != nil {
				log.Printf("Error receiving message from master: %v", err)
				return
			}

			// --- 这里是 Phase 1 的核心逻辑占位符 ---
			switch x := msg.Payload.(type) {
			case *pb.MasterMessage_RegisterResponse:
				log.Printf("Successfully registered! Message from master: %s", x.RegisterResponse.Message)
			case *pb.MasterMessage_TaskAssignment:
				log.Printf("Received new task: %s", x.TaskAssignment.TaskId)
				// TODO: Step 1: 执行任务
				// TODO: Step 2: 发送 StatusUpdate (汇报任务完成)
			default:
				log.Printf("Received unknown message type from master")
			}
			// ---
		}
	}()

	// 6. [保持主线程存活] 
	// 主 goroutine 也需要做一些事情，比如发送心跳
	// 在 Phase 1，我们先用一个定时器模拟发送 StatusUpdate (作为心跳和负载报告)
	ticker := time.NewTicker(10 * time.Second) // 每 10 秒
	defer ticker.Stop()

	for range ticker.C {
		log.Println("Sending periodic status update...")
		updateMsg := &pb.WorkerMessage{
			WorkerId: workerID,
			Payload: &pb.WorkerMessage_StatusUpdate{
				StatusUpdate: &pb.StatusUpdate{ActiveTaskCount: 0},
			},
		}
		if err := stream.Send(updateMsg); err != nil {
			log.Printf("Failed to send status update: %v", err)
		}
	}
}