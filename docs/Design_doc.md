# SwiftScheduler: An Adaptive Distributed Task Scheduler

**Author:** Yilin Zhang  
**Status:** Version 1  
**Reviewers:** Prof. Yiji Zhang  
**Date:** October 20, 2025

## 1. Overview

This document proposes the design for **SwiftScheduler**, a new distributed task scheduling system. Its primary goal is to explore a "sweet spot" in the landscape of task schedulers: combining the operational simplicity of lightweight frameworks (like Celery) with the resilience and intelligence of heavyweight, stream-processing platforms (like Flink).

### The Core Problem

The core problem with existing lightweight frameworks is their **"dumb worker" model**, where workers blindly pull tasks from a message broker. This architecture is highly vulnerable to "thundering herd" scenarios (e.g., flash sales), leading to memory exhaustion and cascading system failure.

### The Solution

SwiftScheduler solves this by introducing a **Master-Worker architecture with a global view**. The Master is the central brain, enabling intelligent flow control and scheduling. The project is divided into two phases:

- **Phase 1 (6-Week Sprint):** Build the core framework and implement a static, global flow controller. The goal is to scientifically prove that this global-view architecture is fundamentally more resilient than the "no-control" baseline model.

- **Phase 2 (Future Work):** Inject dynamic, adaptive intelligence into the proven framework, implementing latency-aware scheduling and dynamic backpressure (Path A & Path B).

This document will detail the complete design, with an immediate, actionable implementation plan for Phase 1.

## 2. Problem & Motivation

Current lightweight task frameworks (Celery, Sidekiq, etc.) are immensely popular for their ease of use. However, their core architecture—where decoupled workers compete to consume tasks from a broker—is a critical liability in high-load, real-world scenarios.

### The Thundering Herd Problem

When a sudden, massive burst of tasks enters the message queue, all available workers will immediately pull and buffer as many tasks as their prefetch limits allow. If the task arrival rate exceeds the cluster's processing capacity, this leads to:

- **Memory Exhaustion (OOM):** Workers buffer tasks in memory, leading to OOM kills
- **Cascading Failure:** Dying workers cause tasks to be re-queued, which are then picked up by other workers, accelerating their failure
- **High p99 Latency:** Even if the system doesn't crash, task-processing latency becomes erratic and unpredictable

### The Gap in the Market

Heavyweight solutions like streaming platforms (Flink) or service meshes (Istio) solve this with native backpressure and intelligent routing, but they introduce an enormous amount of operational complexity, making them unsuitable for simple, real-time task dispatching.

**The "sweet spot" is missing:** A lightweight, easy-to-deploy task scheduler that is natively resilient and intelligent. This project aims to design and validate such a system.

## 3. Goals & Non-Goals

### Goals (Overall Project)

- Design and build a Master-Worker scheduler that leverages a global view for flow control and task routing
- Prove that this architecture is quantifiably more resilient to load spikes than the standard broker-consumer model
- Implement and evaluate adaptive, intelligence-driven scheduling algorithms (Phase 2)

### Non-Goals (For the 6-Week Phase 1 Sprint)

- **No Dynamic Intelligence:** We will not implement any Phase 2 adaptive algorithms (no latency-awareness, no PID controllers). The goal is to prove the static model first
- **No Master High-Availability (HA):** The Master will be a single point of failure (SPOF). This is an acceptable trade-off for a prototype focused on validating the scheduling logic
- **No Feature-Completeness:** We will not implement a full feature set (e.g., cron jobs, task priorities, complex workflows). We are focused purely on the core scheduling/flow-control mechanism
- **No Monitoring UI:** Observability will be handled via logs and benchmark data, not a web UI (like Flower)

## 4. Proposed Architecture

The system consists of four components:

### Client
The task producer. It serializes a task and publishes it to the Broker.

### Broker (RabbitMQ)
The task buffer. It decouples the Client from the scheduler.

### Master (The "Conductor")
This is the core of our system:

- It is the sole consumer from the Broker, giving it a global view of all incoming tasks
- It maintains a real-time state of all connected Workers (WorkerStateManager)
- It implements Macro-Flow Control (Algorithm 1) to protect the entire cluster
- It implements Micro-Scheduling (Algorithm 2) to push tasks to specific Workers

### Worker (The "Executor")

- It is a "dumb" execution unit. It does not connect to the Broker
- On startup, it initiates a bi-directional gRPC stream to the Master to register itself
- It passively waits for the Master to push TaskAssignment messages via the stream
- It reports heartbeats and StatusUpdate messages back to the Master via the same stream

## 5. Detailed Design & Trade-offs (Phase 1)

### 5.1 Key Trade-off: Master-Consumes vs. Worker-Consumes

This is the central design decision of the entire project and our primary departure from Celery.

#### Celery/Worker-Consumes Model
Workers connect directly to the Broker and pull tasks.

**Pros:**
- Simple, decentralized, and naturally load-balanced

**Cons:**
- No global coordination
- Highly vulnerable to "thundering herd" overloads, as all workers will pull tasks simultaneously and crash

#### SwiftScheduler/Master-Consumes Model
The Master is the only component connecting to the Broker.

**Pros:**
- Enables global flow control
- The Master can monitor the entire cluster's health and decide to stop consuming from the Broker, creating backpressure at the source and preventing any worker from being overloaded

**Cons:**
- The Master becomes a potential performance bottleneck and a single point of failure (SPOF)

**Decision:** We choose the Master-Consumes model. Our project's entire hypothesis rests on proving the value of global control and resilience. We accept the trade-off of a centralized bottleneck (a Phase 1 non-goal) to gain this capability.

### 5.2 Communication Protocol (gRPC .proto v1)

We will use gRPC bi-directional streaming for low-latency, persistent communication between the Master and Workers.

```protobuf
syntax = "proto3";

package scheduler;

// The core service. Workers initiate the connection.
service SchedulerService {
  // Bi-directional stream for registration, health/status updates,
  // and task assignments.
  rpc Connect(stream WorkerMessage) returns (stream MasterMessage);
}

// --- Worker -> Master ---
message WorkerMessage {
  string worker_id = 1;
  oneof payload {
    RegisterRequest register_request = 2; // Sent on startup
    StatusUpdate status_update = 3;       // Sent on task completion or for heartbeats
  }
}

message RegisterRequest {
  string hostname = 1;
  int32 max_concurrency = 2; // The max number of tasks this worker can run
}

message StatusUpdate {
  int32 active_task_count = 1; // Current number of tasks being processed
  // In Phase 2, this will be expanded with CPU, p99_latency, etc.
}

// --- Master -> Worker ---
message MasterMessage {
  oneof payload {
    RegisterResponse register_response = 1; // Confirms registration
    TaskAssignment task_assignment = 2;   // Pushes a new task to the worker
  }
}

message RegisterResponse {
  bool success = 1;
  string message = 2;
}

message TaskAssignment {
  string task_id = 1;
  string task_name = 2;
  bytes task_payload = 3; // The serialized task arguments
}
```

### 5.3 Core Algorithms (Phase 1)

#### Algorithm 1: Macro-Flow Control (Static Backpressure)

**Goal:** Protect the entire cluster from overload.

**Implementation:** The Master maintains a GlobalActiveTasks (sum of all active_task_count from all workers) and a GlobalMaxCapacity (sum of all max_concurrency).

**Static Threshold:** We will define a hardcoded high-watermark, e.g., HIGH_WATERMARK = 0.8 (80% capacity).

**Control Loop (in Master's RMQ Consumer):**

```go
// Pseudocode for the Master's consumer loop
for {
    currentLoad := stateManager.GetGlobalActiveTasks() / 
                   stateManager.GetGlobalMaxCapacity()

    if currentLoad >= HIGH_WATERMARK {
        // Load is too high. Apply backpressure.
        // Stop consuming from RabbitMQ.
        rabbitConsumer.Pause()
        // Log that backpressure is active.
        time.Sleep(1 * time.Second) // Wait for the cluster to recover
    } else {
        // Load is acceptable. Resume consumption.
        rabbitConsumer.Resume()
        taskMsg := rabbitConsumer.Poll()
        if taskMsg != nil {
            schedule(taskMsg) // Send to Algorithm 2
        }
    }
}
```

#### Algorithm 2: Micro-Scheduling (Capacity-Aware Round-Robin)

**Goal:** Distribute tasks fairly to available workers.

**Implementation:** The Master will not do a simple round-robin. It will perform a round-robin only on the subset of workers that are currently not at full capacity (i.e., active_task_count < max_concurrency). If all workers are at capacity, the task will wait in the Master's internal memory (briefly, as the flow control loop should have already paused consumption).

## 6. Testing & Validation Plan (Phase 1)

### Control Group (Baseline)
A "Celery-like" model. We will write a simple Go script that launches N separate processes, each acting as an independent consumer pulling tasks directly from RabbitMQ.

### Experimental Group (Candidate)
Our Phase 1 Master-Worker system with N workers.

### Test Scenario: "Thundering Herd" (Resilience Test)

**Load:** A load generator script will publish 10,000 tasks (each simulating 500ms of work) to RabbitMQ in under 1 second.

### Metrics

- **System Stability (Primary):** Do the worker processes crash (OOM)? Does the system recover?
- **Throughput Curve:** A graph of completed tasks over time
- **Resource Usage:** CPU and Memory graphs for the worker processes

### Expected Outcome (for the report)
We will show a graph where the Baseline workers' memory usage spikes and the processes crash. In contrast, the Candidate (our system) workers' memory will remain stable, and the throughput curve will be a clean, steady line as it processes tasks at its maximum controlled capacity. 

**Conclusion:** Phase 1's global view provides quantifiable, superior resilience.

## 7. Milestones (6-Week Sprint: Oct 21 - Nov 30)

- **Week 1 (Oct 21-26):** Finalize this Design Doc. Set up project skeleton (Go modules), Git repo. Define and generate .proto v1 code.

- **Week 2 (Oct 27-Nov 2):** Implement baseline gRPC Connect stream between Master and Worker. Implement Worker RegisterRequest logic.

- **Week 3 (Nov 3-Nov 9):** Implement Master's WorkerStateManager (for registration, heartbeats). Implement Master's basic RabbitMQ consumer loop.

- **Week 4 (Oct 10-Nov 16):** Implement Core Loop: Master can TaskAssignment -> Worker, Worker executes (simulated) and reports StatusUpdate (with active_task_count).

- **Week 5 (Nov 17-Nov 23):** Implement Core Innovation: Implement Algorithm 1 (Static Backpressure). Build the Baseline script and the Thundering Herd load generator. Run all experiments and collect data/graphs.

- **Week 6 (Nov 24-Nov 30):** Analyze data. Write the final report and create the presentation slides. Rehearse.

## 8. Future Work (Phase 2)

Upon the successful validation of the Phase 1 framework, Phase 2 will inject dynamic intelligence. The HealthReport message will be expanded to include CPU, I/O, and p99 latency metrics. The Master's static algorithms will be replaced with:

### Path A (Intelligent Load Definition)
A multi-dimensional "health score" will be calculated for each worker.

### Path B (Dynamic & Adaptive Thresholds)
The Micro-Scheduler (Algorithm 2) will be upgraded to route tasks to the worker with the best health score. The Macro-Flow Controller (Algorithm 1) will be upgraded to use a PID controller to dynamically adjust consumption, targeting an optimal cluster load.

## 9. Alternatives Considered

### Alternative: Use Celery + Kubernetes HPA

**Why not?** HPA is reactive and slow (minute-scale), designed to solve scale. Our problem is proactive and instantaneous (millisecond-scale), designed to solve overload. HPA cannot prevent workers from crashing in the first 30 seconds of a load spike.

### Alternative: Decentralized Control (Workers coordinate)

**Why not?** Requires complex gossip protocols for workers to share state. A centralized Master is a simpler, more deterministic, and more powerful model for achieving a true global view. The SPOF trade-off is acceptable for this project's goals.
