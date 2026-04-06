package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/YilinZhang0101/SwiftScheduler/internal/adaptive"
	pb "github.com/YilinZhang0101/SwiftScheduler/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

var activeTaskCount int32

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

func parseBoolEnv(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	case "0", "false", "FALSE", "no", "NO", "off", "OFF":
		return false
	default:
		log.Printf("invalid bool for %s=%q, using default %v", key, v, def)
		return def
	}
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func shouldReconfigure(cur, next keepalive.ClientParameters, minDelta time.Duration) bool {
	return absDuration(cur.Time-next.Time) >= minDelta || absDuration(cur.Timeout-next.Timeout) >= minDelta
}

func main() {
	masterAddr := os.Getenv("MASTER_ADDR")
	if masterAddr == "" {
		masterAddr = "localhost:50051"
	}

	kaParamsClient := keepalive.ClientParameters{
		Time:                parseDurationEnv("WORKER_KA_TIME", 10*time.Second),
		Timeout:             parseDurationEnv("WORKER_KA_TIMEOUT", 5*time.Second),
		PermitWithoutStream: true,
	}
	maxConc := int32(parseIntEnv("WORKER_MAX_CONCURRENCY", 10))
	reconnectBackoff := parseDurationEnv("WORKER_RECONNECT_BACKOFF", 2*time.Second)

	adaptiveEnabled := parseBoolEnv("WORKER_ADAPTIVE_ENABLED", false)
	adaptiveReconnectEvery := parseDurationEnv("WORKER_ADAPTIVE_RECONNECT_EVERY", 2*time.Minute)
	adaptiveMinSamples := parseIntEnv("WORKER_ADAPTIVE_MIN_SAMPLES", 20)
	adaptiveMinDelta := parseDurationEnv("WORKER_ADAPTIVE_MIN_DELTA", 500*time.Millisecond)
	estimator := adaptive.NewRTTEstimator()

	log.Printf("[Worker] master=%s ka_time=%v ka_timeout=%v max_concurrency=%d",
		masterAddr, kaParamsClient.Time, kaParamsClient.Timeout, maxConc)
	log.Printf("[Worker] adaptive_enabled=%v reconnect_every=%v min_samples=%d min_delta=%v",
		adaptiveEnabled, adaptiveReconnectEvery, adaptiveMinSamples, adaptiveMinDelta)

	hostname, _ := os.Hostname()
	workerID := fmt.Sprintf("%s-%d", hostname, os.Getpid())

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-quit
		log.Printf("[Death] worker=%s time=%s signal=%v",
			workerID,
			time.Now().UTC().Format(time.RFC3339Nano),
			sig,
		)
		os.Exit(0)
	}()

	for attempt := 1; ; attempt++ {
		nextKA, err := runWorkerSession(
			masterAddr,
			workerID,
			maxConc,
			kaParamsClient,
			adaptiveEnabled,
			adaptiveReconnectEvery,
			adaptiveMinSamples,
			adaptiveMinDelta,
			estimator,
		)
		if nextKA != nil {
			log.Printf("[Worker][Adaptive] applying new keepalive params on reconnect: time=%v timeout=%v", nextKA.Time, nextKA.Timeout)
			kaParamsClient = *nextKA
		}

		if err != nil && !errors.Is(err, io.EOF) {
			log.Printf("[Worker] session ended with error: %v", err)
		}

		log.Printf("[Worker] reconnecting in %v (attempt=%d)", reconnectBackoff, attempt)
		time.Sleep(reconnectBackoff)
	}
}

func runWorkerSession(
	masterAddr string,
	workerID string,
	maxConc int32,
	kaParamsClient keepalive.ClientParameters,
	adaptiveEnabled bool,
	adaptiveReconnectEvery time.Duration,
	adaptiveMinSamples int,
	adaptiveMinDelta time.Duration,
	estimator *adaptive.RTTEstimator,
) (*keepalive.ClientParameters, error) {
	conn, err := grpc.Dial(
		masterAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(kaParamsClient),
	)
	if err != nil {
		return nil, fmt.Errorf("dial master: %w", err)
	}
	defer conn.Close()

	client := pb.NewSchedulerServiceClient(conn)
	stream, err := client.Connect(context.Background())
	if err != nil {
		return nil, fmt.Errorf("open stream: %w", err)
	}

	connectStart := time.Now()
	sessionStart := connectStart
	sem := make(chan struct{}, maxConc)
	var sendMu sync.Mutex

	sendStatus := func() error {
		sendMu.Lock()
		defer sendMu.Unlock()

		current := atomic.LoadInt32(&activeTaskCount)
		updateMsg := &pb.WorkerMessage{
			WorkerId: workerID,
			Payload: &pb.WorkerMessage_StatusUpdate{
				StatusUpdate: &pb.StatusUpdate{
					ActiveTaskCount: current,
				},
			},
		}
		return stream.Send(updateMsg)
	}

	req := &pb.WorkerMessage{
		WorkerId: workerID,
		Payload: &pb.WorkerMessage_RegisterRequest{
			RegisterRequest: &pb.RegisterRequest{
				Hostname:       workerID,
				MaxConcurrency: maxConc,
			},
		},
	}
	sendMu.Lock()
	if err := stream.Send(req); err != nil {
		sendMu.Unlock()
		return nil, fmt.Errorf("send register request: %w", err)
	}
	sendMu.Unlock()
	log.Printf("[Worker] %s sent register request.", workerID)

	recvErrCh := make(chan error, 1)
	go func() {
		log.Printf("[Worker] recvLoop started")
		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				detectAt := time.Now()
				log.Printf("[Detect] side=worker worker=%s time=%s type=EOF elapsed=%v",
					workerID,
					detectAt.UTC().Format(time.RFC3339Nano),
					detectAt.Sub(connectStart),
				)
				recvErrCh <- io.EOF
				return
			}
			if err != nil {
				detectAt := time.Now()
				log.Printf("[Detect] side=worker worker=%s time=%s type=ERR err=%v elapsed=%v",
					workerID,
					detectAt.UTC().Format(time.RFC3339Nano),
					err,
					detectAt.Sub(connectStart),
				)
				recvErrCh <- err
				return
			}

			switch x := msg.Payload.(type) {
			case *pb.MasterMessage_RegisterResponse:
				log.Printf("[Worker] registered: %s", x.RegisterResponse.Message)
			case *pb.MasterMessage_TaskAssignment:
				task := x.TaskAssignment
				log.Printf("[Worker] got task=%s", task.TaskId)

				sem <- struct{}{}
				atomic.AddInt32(&activeTaskCount, 1)
				if err := sendStatus(); err != nil {
					log.Printf("[Worker] status send on start failed: %v", err)
				}

				go func(t *pb.TaskAssignment) {
					defer func() {
						atomic.AddInt32(&activeTaskCount, -1)
						<-sem
						if err := sendStatus(); err != nil {
							log.Printf("[Worker] status send on finish failed: %v", err)
						}
					}()
					log.Printf("[Start] task=%s", t.TaskId)
					time.Sleep(3 * time.Second)
					log.Printf("[Finish] task=%s", t.TaskId)
				}(task)
			default:
				log.Printf("[Worker] unknown message type")
			}
		}
	}()

	hbEvery := parseDurationEnv("WORKER_HEARTBEAT", 2*time.Second)
	ticker := time.NewTicker(hbEvery)
	defer ticker.Stop()
	var lastStatusSent time.Time

	for {
		select {
		case recvErr := <-recvErrCh:
			return nil, recvErr
		case <-ticker.C:
			now := time.Now()
			if err := sendStatus(); err != nil {
				detectAt := time.Now()
				log.Printf("[Detect] side=worker worker=%s time=%s type=SEND_ERR err=%v elapsed=%v",
					workerID,
					detectAt.UTC().Format(time.RFC3339Nano),
					err,
					detectAt.Sub(connectStart),
				)
				return nil, err
			}

			if adaptiveEnabled && !lastStatusSent.IsZero() {
				estimator.Update(now.Sub(lastStatusSent))
			}
			lastStatusSent = now

			if adaptiveEnabled && time.Since(sessionStart) >= adaptiveReconnectEvery && estimator.NSamples() >= adaptiveMinSamples {
				rec := estimator.Recommend()
				next := keepalive.ClientParameters{
					Time:                rec.KATime,
					Timeout:             rec.KATimeout,
					PermitWithoutStream: true,
				}
				if shouldReconfigure(kaParamsClient, next, adaptiveMinDelta) {
					log.Printf("[Worker][Adaptive] samples=%d srtt=%v rttvar=%v recommend_time=%v recommend_timeout=%v confidence=%s",
						estimator.NSamples(), estimator.SRTT(), estimator.RTTVAR(), rec.KATime, rec.KATimeout, rec.Confidence)
					return &next, nil
				}
				sessionStart = time.Now()
			}
		}
	}
}
