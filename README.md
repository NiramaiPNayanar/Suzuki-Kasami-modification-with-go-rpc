# Suzuki-Kasami-modification-with-go-rpc

1. No Explicit RN (Request Number) Vector
Suzuki-Kasami maintains an RN[] array where RN[i] is the highest sequence number received from process i.

This implementation uses a priority queue instead, ordering requests by:
Sequence number (like Lamport timestamps)
Request time (for tie-breaking)
Process ID (for further tie-breaking)

This removes the need for a distributed RN vector, simplifying state management.

2. No LN (Last Number) Array for Token

Suzuki-Kasami tracks LN[i] (the last sequence number for which process i was granted the token).
This implementation does not track LN explicitly; instead, it relies on the priority queue to ensure fairness.

3. Timeout-Based Token Recovery :

Suzuki-Kasami assumes no failures—if a process crashes while holding the token, the system deadlocks.

This implementation adds timeout-based recovery:
If a client holds the token for too long (TokenTimeout = 10s), the server revokes it.
If a client fails (FailureTimeout = 20s), the server removes it from the active set.

4. Heartbeat Mechanism for Liveness :
Suzuki-Kasami does not handle process failures.

This implementation includes:
Heartbeats (HandleClientHeartbeat RPC) to detect live clients.
Graceful exit handling (ClientExiting RPC) for clean shutdowns.

5. Fairness Enforcement (Aging) :
Suzuki-Kasami grants the token in FIFO order based on RN[].

This implementation adds aging:
If a request waits too long (>15s), it gets prioritized (enforceFairness goroutine).
Prevents starvation of slow or unlucky clients.

6. No Broadcast Mechanism :
Suzuki-Kasami requires broadcasting requests to all processes.

This implementation is centralized:
Clients send requests only to the server (no flooding).
More efficient in small-to-medium systems but introduces a single point of failure.

7. Idle Token Handling :
Suzuki-Kasami does not handle cases where the token is unused.
This implementation resets the token if it remains idle for IdleCheckPeriod (30s).


