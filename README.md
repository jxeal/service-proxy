
# Distributed Health Monitor & Reverse Proxy

A concurrent reverse proxy and active health monitor built entirely with the Go standard library. 

## Overview
This application consists of two main components running concurrently:
1. **Background Health Scanner:** A Goroutine-based scanner that iterates through a pool of upstream servers on a 30-second interval. It performs HTTP health checks to evaluate node availability and measures response latency in milliseconds.
2. **Dynamic Reverse Proxy:** An HTTP proxy that intercepts incoming requests and evaluates the shared state registry. It dynamically rewrites request targets to route traffic to the upstream node with the lowest measured latency.

State is maintained in a shared registry protected by a `sync.RWMutex`, allowing the proxy layer to handle high read throughput without data races during background state updates.

## Motivation
Standard load balancing algorithms, such as round-robin, distribute traffic evenly but blindly. If an upstream node experiences degraded performance or fails, a standard proxy may continue routing requests to it, resulting in timeouts or errors. 

This project implements a **latency-aware routing strategy**. By continuously monitoring upstream health in the background, the proxy dynamically shifts traffic away from slow or failing nodes. It provides a lightweight, zero-dependency alternative to heavier service mesh tools for simple upstream routing.

## Usage

### 1. Start the Server
Requires Go 1.21+. Clone the repository and run the main file:
```bash
go run main.go
```
The CLI dashboard will initialize, run the first health scan, and output the status and latency of the configured upstream servers.

### 2. Send HTTP Traffic
Send a request to the local proxy. It will be forwarded to the most optimal upstream node.

**Mac/Linux:**
```bash
curl -i http://localhost:8080/get
```
**Windows (PowerShell):**
```powershell
curl.exe -i http://localhost:8080/get
```

### 3. Verify Routing Behavior
Check the standard output of the Go server. The `Director` function logs routing decisions in real-time, indicating the original request path and the selected upstream target:
```text
[PROXY] Rerouting request for '/get' -> https://httpbin.org
```
*(Note: The terminal dashboard refreshes every 30 seconds upon scan completion, which will clear previous routing logs).*

### 4. Graceful Shutdown
Send a `SIGINT` (via `Ctrl+C`) to terminate the process. The server uses context cancellation to stop the background scanner and allows a 5-second timeout window to drain any active HTTP connections before exiting.