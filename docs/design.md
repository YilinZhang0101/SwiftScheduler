# Adaptive Keepalive Tuning for Fast and Robust Failure Detection in gRPC-Based Distributed Systems

## 1. Motivation
### 1.1 Background: 
Modern distributed scheduling systems frequently rely on long-lived gRPC connections between Master and Worker nodes. These persistent HTTP/2 connections enable Workers to receive tasks continuously while reporting real-time status or runtime metadata back to the Master. Ensuring connection liveness is therefore fundamental to system reliability and task correctness.

gRPC provides a built-in Keepalive mechanism that periodically sends HTTP/2 PING frames to validate a connection. If a PING response is not received within a specified timeout, the connection is assumed dead and is closed.

### 1.2 Challenge: 
Static Keepalive Leads to Slow and Inaccurate Failure Detection

Despite its simplicity, gRPC’s Keepalive mechanism suffers from static, one-size-fits-all timing parameters. In real distributed environments, Workers may experience:

- fluctuating RTT (network jitter),

- transient packet loss,

- temporary congestion,

- or silent crashes (process freeze or OOM kills).

### Real-world problem
For instance, a Worker silently crashed. 

However, due to a fixed keepalive_timeout of 20 seconds, the Master continued to dispatch tasks to it under the false assumption that it was still healthy. 

During this task loss window, all dispatched tasks failed. Even worse, since no new heartbeat updates were received, the Master incorrectly calculated that this Worker had the lowest load, exacerbating the issue by directing even more traffic to a dead node.

### 1.3 Key Limitations：

Fixed Keepalive parameters are insufficient because:

- They cannot adapt to varying network conditions.

- Large static timeout → unnecessarily slow failure detection.

- Small static timeout → high false positives in jitter-heavy networks.

- They ignore historical failure trends or transient network quality.

Thus, a static Keepalive mechanism is unsuitable for distributed scheduling systems requiring fast, accurate, and network-aware failure detection.


## 2. Purpose

### 2.1 Adaptive gRPC Keepalive Tuning

An Adaptive Keepalive Tuning Mechanism that dynamically adjusts:

- keepalive_time

- keepalive_timeout

based on real-time network behavior between Master and Worker nodes.

Instead of using fixed thresholds, the mechanism continuously monitors:

- RTT_mean(t) — average round-trip time

- RTT_std(t) — variance or network jitter

- FailRate(t) — historical failure rate (PING timeouts / errors)

These metrics provide an accurate reflection of network health.

### 2.2 Adaptive Tuning Formulas
'''
keepalive_time =
    base_time
    + α × RTT_mean(t)
    + β × RTT_std(t)

keepalive_timeout =
    base_timeout
    + γ × RTT_mean(t)
    + δ × FailRate(t) × base_timeout
'''

### 2.3 Final Goal

This work contributes:

- A unified adaptive tuning model for gRPC Keepalive.

- Significantly faster silent crash detection under real-world network noise.

- Reduced false positives in high-jitter networks.

- Minimal overhead and full compatibility with existing gRPC APIs.

- Comprehensive evaluation showing improvements in detection latency and task loss window.


## 3. Literature Review

### 3.1 gRPC Keepalive: Strengths and Limitations

gRPC maintains long-lived HTTP/2 channels using a built-in Keepalive mechanism that periodically sends PING frames to verify transport-level reachability. Two parameters govern its behavior:

- keepalive_time, the frequency of PING probes, and

- keepalive_timeout, the maximum wait time before the connection is deemed failed.

Although simple, this mechanism relies entirely on static, globally fixed thresholds, which makes it poorly suited for volatile network environments. Static thresholds force practitioners to choose between aggressive settings—which detect failures quickly but trigger spurious disconnects under transient latency spikes—and conservative settings, which avoid false alarms but significantly delay the detection of silent failures. In distributed scheduling systems, this often results in extended “silent failure windows,” during which a dead Worker continues receiving tasks until the default timeout finally triggers. These limitations indicate the need for an adaptive, network-aware approach to transport-level failure detection.

### 3.2 Stability and Cut Detection in Membership Systems

The challenges posed by static failure-detection thresholds are well-documented in cluster membership research. Rapid, a production-grade membership system, demonstrates that reacting to isolated or noisy failure signals leads to instability, especially under conditions such as one-way reachability, asymmetric packet loss (Suresh et al., 2018), or firewall interference. Instead of relying on a single timeout event, Rapid aggregates corroborating alerts from multiple sources and treats correlated network problems as a multi-process cut, which prevents “view flapping” and ensures consistent membership changes.

While Rapid operates at the membership layer rather than the transport layer, its findings establish two principles directly relevant to gRPC Keepalive:

- A single static threshold is inherently unreliable under noisy or asymmetric network conditions, and

- Stable failure detection requires incorporating broader context rather than reacting to one signal.

These insights motivate the design of a Keepalive mechanism that dynamically adapts probe timing and timeout thresholds based on real-time network behavior rather than depending solely on fixed values.

### 3.3 Adaptive Timeout Models and Slow-Fault Sensitivity

Recent systems research further highlights the inadequacy of static threshold-based failure detection. Lu et al. (2025) perform the first large-scale characterization of fail-slow behavior—a common but historically under-recognized failure mode in modern distributed systems. Through systematic fault injection into production-grade runtimes, the authors show that system behavior is highly sensitive to small variations in latency, throughput, or packet loss, and that most existing detection frameworks activate only under severe degradations.

To address this, they introduce ADR, a lightweight, runtime-level mechanism that dynamically adjusts thresholds based on observed performance. ADR’s central contribution is demonstrating that:

- System health lies on a continuum rather than a binary state,

- Static timeouts are incapable of capturing intermediate performance degradations, and

- Failure detection improves substantially when thresholds incorporate RTT distributions, variance, and historical behavior.

These results directly parallel the limitations of fixed keepalive_timeout values in gRPC Keepalive and reinforce the need for a feedback-driven timeout strategy that reacts proportionally to the degree of performance degradation.

### 3.4 Research Gap

The two bodies of related work offer complementary insights:

- Rapid shows that reliable failure detection requires multi-signal reasoning and resistance to noisy or partial failures, avoiding premature or inconsistent conclusions.

- Slow-fault research (ADR) demonstrates that static thresholds cannot accommodate performance variability, and dynamically tuned thresholds significantly reduce both false positives and missed detections.

However, neither line of work addresses failure detection at the level of transport-layer gRPC connections, where Keepalive PINGs and timeouts remain static and untuned. Existing systems research provides conceptual foundations—such as multi-source stability and adaptive thresholding—but does not translate these principles into a concrete mechanism for transport-layer Keepalive tuning.

This gap motivates the present work: the design of an RTT-aware and failure-trend-aware Adaptive Keepalive mechanism capable of adjusting keepalive_time and keepalive_timeout dynamically. Such a design aims to improve silent-failure detection, reduce false positives caused by jitter, and minimize the task-loss window in distributed scheduling systems.

### References:
Lu, Y., Chen, Y., Chen, H., Zhang, H., & Zhao, M. (2025). Understanding slow faults in modern distributed systems. In Proceedings of the 22nd USENIX Symposium on Networked Systems Design and Implementation (NSDI ’25) (pp. 349–371). USENIX Association.
https://www.usenix.org/conference/nsdi25/presentation/lu

Suresh, L., Malkhi, D., Gopalan, P., Porto Carreiro, I., & Lokhandwala, Z. (2018). Stable and consistent membership at scale with Rapid. In Proceedings of the 2018 USENIX Annual Technical Conference (USENIX ATC ’18) (pp. 387–401). USENIX Association.
https://www.usenix.org/conference/atc18/presentation/suresh

Chandra, T. D., & Toueg, S. (1996). Unreliable failure detectors for reliable distributed systems. Journal of the ACM, 43(2), 225–267.

Jacobson, V. (1988). Congestion avoidance and control. In Proceedings of the ACM SIGCOMM 1988 conference (pp. 314–329). ACM.

## 4. Progress
### 4.1 Completed Bidirectional gRPC Streaming Channel
#### Worker Side

- Establishes a persistent gRPC streaming connection to the Master

- Sends an initial RegisterRequest to introduce itself

- Periodically sends StatusUpdate messages representing Worker load and status

#### Master Side

- Receives and stores Worker registration information

- Maintains active bidirectional streaming sessions

- Detects disconnections and automatically unregisters a Worker


### 4.2 Fully Functional Worker Registration and Global State Management

A thread-safe StateManager has been implemented, providing:

- Worker registration and deregistration

- Real-time Worker state updates (e.g., ActiveTaskCount)

- Global view of Worker capacity and load

- Least-loaded Worker selection for scheduling


### 4.3 End-to-End Task Dispatching Pipeline Is Complete

The Master integrates seamlessly with RabbitMQ to fetch tasks, select an appropriate Worker, and push tasks through the gRPC stream.

Master reads tasks from RabbitMQ -> Master selects a Worker using Least Load -> Master dispatches the task through the active gRPC stream -> Worker executes tasks asynchronously -> Worker reports status updates back to the Master

### 4.4 Heartbeat (StatusUpdate) Prototype Working

Workers currently send periodic status updates every 5 seconds, reporting their active task count. (application-layer heartbeat)

## 5. To Be Realize

### 5.1 Collect Real RTT Samples from gRPC Keepalive

- Add RTT logging for Keepalive PING → PING-ACK

- Or hook into gRPC’s native Keepalive callback functionality

- Maintain a sliding window of RTT samples

### 5.2 Implement Failure-Rate Tracking (FailRate)
### 5.3 Implement the Adaptive Timeout Computation Module
### 5.4 Dynamically Apply Keepalive Parameters in gRPC Runtime

## 5 Experimental Evaluation
- RTT Jitter Sensitivity
- Silent Worker Crash
- Task Loss Window