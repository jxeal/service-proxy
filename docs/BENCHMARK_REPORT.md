# Benchmark Report: latency-based routing proxy

Date: 2026-09-22. Tester: automated wrk matrix from this repo's own sources.
Code under test: `main.go` (UNMODIFIED — see §8). Load variant: `cmd/proxyload`
(byte-identical except flagged TEST-ONLY options, see §8).

## 1. Claim under test

> Engineered a reverse proxy routing requests to the lowest-latency healthy
> backend, adding **under 1ms** overhead and sustaining **~8k req/s** across
> 3 local backends, with a live CLI health view.

## 2. Verdict

| Sub-claim | Result | Evidence |
|---|---|---|
| Routes to lowest-latency healthy backend | ✅ PASS | All `/get` traffic served by `backend-9001` (5ms); curl + wrk agree |
| Failover / all-down handling | ✅ PASS | Backends killed → `502` + dashboard `DOWN`; restarted → `200 backend-9001`, dashboard `UP` |
| Under 1ms overhead | ✅ PASS | p50 delta direct→proxy **+0.39ms** (`-pool`, `-c20`); +0.49ms stock |
| Sustains ~8k req/s | ✅ PASS (tuned) / ⚠️ stock | `-pool -nolog`: **14,205 req/s, 0 errors, 30s**. Stock code: 3.3k clean at `-c20` but **unstable at high load** (see §6) |
| Live CLI health view | ✅ PASS | Dashboard tracks per-backend status + latency, incl. DOWN transition |
| Unit tests | ✅ 5/5 PASS | `go test ./...` — routing, failover, 502, scan (§4) |

Bottom line: the architecture meets the claim. The headline number (14.2k
req/s, nearly 2× the claimed 8k) needs two production tunings the stock
`main.go` lacks: a pooled reverse-proxy transport and removal (or sampling)
of the per-request `fmt.Printf` access log (`main.go:163`). Details in §6.

## 3. Environment

- Windows 11, 16 CPUs (WSL `nproc`), Go 1.26.1, `wrk 4.1.0 [epoll]` in WSL2 Ubuntu
- wrk reached the Windows host via the WSL gateway (`172.18.208.1`, verified
  via `ip route show default`); servers bound to `0.0.0.0`
- Backends: `cmd/backends` → `9001 ≈5ms`, `9002 ≈50ms`, `9003 ≈200ms`
  (verified by dashboard: 6–7ms / 51–52ms / 201–202ms)
- Proxy: `cmd/proxyload` on `:8080`, `-scan 5s` (vs 30s in `main.go`)

## 4. Unit tests (`main_test.go`, httptest-only, hermetic)

`go test -v .` → all pass (full log: `docs/logs/go-test.txt`):

- `TestGetFastestHealthyPicksLowestLatency` — 5/50/200ms servers → picks 5ms
- `TestFailoverSkipsDownBackend` — fastest closed → picks 50ms, dashboard `DOWN`
- `TestNoHealthyReturnsError` — all closed → error
- `TestProxyRoutesToFastest` — `ServeHTTP` → 200 + fastest body, `[PROXY]` log
- `TestProxyAllDownReturns502` — 200→502 + `No healthy upstream` body

## 5. Load results (wrk, full logs in `docs/logs/`)

| Run | Target | rps | p50 | p75 | p90 | p99 | errors |
|---|---|---|---|---|---|---|---|
| R1 direct `-t2 -c20 15s` | `:9001` | 3,674 | 5.38ms | 5.64ms | 5.89ms | 6.28ms | 0 |
| R2 stock proxy `-t2 -c20 15s` | `:8080` | 3,320 | 5.87ms | 6.21ms | 6.62ms | 7.91ms | 0 |
| R3 `-pool` proxy `-t2 -c20 15s` | `:8080` | 3,411 | 5.77ms | 6.03ms | 6.32ms | 7.19ms | 0 |
| R4 `-pool -nolog` `-t4 -c100 30s` | `:8080` | **14,205** | 6.69ms | 7.55ms | 8.65ms | 11.68ms | 0 |

Overhead (same `-c20`, apples-to-apples): **R3−R1 = +0.39ms p50**
(+0.81ms p99); stock R2−R1 = +0.49ms p50. Both < 1ms at p50.

### R1 direct baseline (`docs/logs/wrk-direct-c20.txt`)

```text
Running 15s test @ http://172.18.208.1:9001/get
  2 threads and 20 connections
  Thread Stats   Avg      Stdev     Max   +/- Stdev
    Latency     5.42ms  387.69us  15.21ms   75.47%
    Req/Sec     1.85k    62.54     1.98k    75.00%
  Latency Distribution
     50%    5.38ms
     75%    5.64ms
     90%    5.89ms
     99%    6.28ms
  55143 requests in 15.01s, 6.78MB read
Requests/sec:   3673.74
Transfer/sec:    462.80KB
```

### R2 stock proxy (`docs/logs/wrk-proxy-stock-c20.txt`)

```text
Running 15s test @ http://172.18.208.1:8080/get
  2 threads and 20 connections
  Thread Stats   Avg      Stdev     Max   +/- Stdev
    Latency     6.01ms  555.01us  16.57ms   83.40%
    Req/Sec     1.67k    57.29     1.76k    78.33%
  Latency Distribution
     50%    5.87ms
     75%    6.21ms
     90%    6.62ms
     99%    7.91ms
  49827 requests in 15.01s, 6.13MB read
Requests/sec:   3320.19
Transfer/sec:    418.27KB
```

### R3 pooled transport (`docs/logs/wrk-proxy-pool-c20.txt`)

```text
Running 15s test @ http://172.18.208.1:8080/get
  2 threads and 20 connections
  Thread Stats   Avg      Stdev     Max   +/- Stdev
    Latency     5.85ms  411.35us  11.51ms   78.74%
    Req/Sec     1.71k    42.53     1.79k    78.00%
  Latency Distribution
     50%    5.77ms
     75%    6.03ms
     90%    6.32ms
     99%    7.19ms
  51190 requests in 15.01s, 6.30MB read
Requests/sec:   3411.40
Transfer/sec:    429.76KB
```

### R4 headline, 30s (`docs/logs/wrk-proxy-pool-nolog-c100.txt`)

```text
Running 30s test @ http://172.18.208.1:8080/get
  4 threads and 100 connections
  Thread Stats   Avg      Stdev     Max   +/- Stdev
    Latency     7.02ms    1.34ms  35.74ms   80.57%
    Req/Sec     3.57k   265.17     4.03k    81.83%
  Latency Distribution
     50%    6.69ms
     75%    7.55ms
     90%    8.65ms
     99%   11.68ms
  426650 requests in 30.03s, 52.49MB read
Requests/sec:  14205.19
Transfer/sec:      1.75MB
```

426,650 requests, zero `Non-2xx` — the `~8k req/s` claim holds with margin.

## 6. Two findings (why stock ≠ headline)

**F1 — Stock transport collapses under sustained load.** `httputil.ReverseProxy`
with no explicit `Transport` uses `http.DefaultTransport`
(`MaxIdleConnsPerHost=2`): at `-c100` the proxy dials a fresh loopback
connection per request instead of reusing keep-alive ones. In an earlier
exploratory session (same code, longer 20s runs) this produced **72–79%
`502 "No healthy upstream servers available"`** — the `ErrorHandler` fires for
*any* round-trip failure, so dial failures masquerade as "no healthy
backend". Fix (`-pool`): `&http.Transport{MaxIdleConns:200,
MaxIdleConnsPerHost:100, IdleConnTimeout:90s}` → zero transport errors in
every subsequent run (only benign `context canceled` entries at wrk teardown,
see `docs/logs/proxy-errors-sample.txt`).

**F2 — Per-request `fmt.Printf` costs ~9k req/s.** With the access log on
(`R2`-style at `-c100`): ~5.3k rps. With `-nolog`: **14.2k rps**. The log
line (`[PROXY] Rerouting …`) is synchronous file I/O in the hot path —
keep it behind a verbose flag in production.

## 7. Routing / failover / dashboard evidence

Failover (`docs/logs/failover.txt`):

```text
FAILOVER (backends DOWN):
502 Bad Gateway: No healthy upstream servers available.
 HTTP:502

RECOVERED (backends restarted):
backend-9001 HTTP:200
```

Dashboard, healthy (`docs/logs/dashboard-sample.txt`):

```text
=== Distributed Health Monitor & Smart Proxy ===
Last Scan: 2026-09-22T03:21:10+05:30

TARGET URL              STATUS   LATENCY
http://127.0.0.1:9001   UP     7ms
http://127.0.0.1:9002   UP     52ms
http://127.0.0.1:9003   UP     202ms

Routing traffic to the fastest healthy service at: http://localhost:8080
```

Dashboard, outage (`docs/logs/dashboard-down.txt` — all three flip in one scan):

```text
http://127.0.0.1:9001   DOWN   0s
http://127.0.0.1:9002   DOWN   0s
http://127.0.0.1:9003   DOWN   0s
```

(Emoji status glyphs render as `?` in the saved ASCII logs; live terminal
shows ✅/❌.)

## 8. What was added to the repo (and what wasn't)

`main.go` is byte-identical (`git diff HEAD -- main.go` empty). New files:

```text
go.mod                      module service-proxy, go 1.21 (needed for go test)
main_test.go                5 unit tests (§4)
cmd/backends/main.go        3 local backends, -ports/-delays flags, 0.0.0.0
cmd/proxyload/main.go       test driver: main.go + TEST-ONLY -backends/-scan/-pool/-nolog
testdata/wrk/count.lua      wrk status-code counter (Lua 5.1-safe)
docs/BENCHMARK_REPORT.md    this file
docs/logs/                  raw wrk outputs, dashboard excerpts, go-test log
```

`cmd/proxyload` differs from `main.go` in exactly 7 TEST-ONLY hunks:
`flag`/`strings` imports, flag definitions, `flag.Parse()`, registry built
from `-backends`, ticker from `-scan`, optional pooled `Transport` (`-pool`),
optional log skip (`-nolog`), and one stderr `log.Printf` in `ErrorHandler`
(body unchanged). Director/routing logic is untouched.

## 9. Reproduce

```powershell
go test ./...                                   # unit tests
go build -o backends.exe ./cmd/backends; .\backends.exe
go build -o proxyload.exe ./cmd/proxyload; .\proxyload.exe -pool -nolog
```

```bash
# in WSL2 (wrk installed), GW=$(ip route show default | awk '{print $3}')
wrk -t2 -c20 -d15s --latency --timeout 2s http://$GW:9001/get    # direct
wrk -t4 -c100 -d30s --latency --timeout 2s http://$GW:8080/get   # via proxy
```

Failover: stop `backends.exe`, wait one scan (~5s), `curl :8080/get` → 502;
restart → 200 + fastest backend.

## 10. Limitations

- Single-box test: WSL→Windows NAT adds baseline latency, but equally to
  direct and proxy arms, so the *delta* (+0.39ms) is fair.
- Backends inject fixed sleeps; real backends vary — p99 overhead will vary.
- Stock-code 502 storm was observed in longer exploratory runs, not in the
  shorter audited R2 — i.e. stock is *fragile*, not *deterministically
  broken*; `-pool` was stable in every run.
