package main

import (
	"container/heap"
	"fmt"
	"log"
	"net"
	"net/rpc"
	"sync"
	"time"
)

const (
	TokenTimeout    = 10 * time.Second
	FailureTimeout  = 20 * time.Second
	IdleCheckPeriod = 30 * time.Second
)

type Request struct {
	SeqNum      int
	ProcessID   int
	RequestTime time.Time
}

type PriorityQueue []*Request

func (pq PriorityQueue) Len() int { return len(pq) }
func (pq PriorityQueue) Less(i, j int) bool {
	if pq[i].SeqNum == pq[j].SeqNum {
		if pq[i].RequestTime.Equal(pq[j].RequestTime) {
			return pq[i].ProcessID < pq[j].ProcessID
		}
		return pq[i].RequestTime.Before(pq[j].RequestTime)
	}
	return pq[i].SeqNum < pq[j].SeqNum
}
func (pq PriorityQueue) Swap(i, j int) { pq[i], pq[j] = pq[j], pq[i] }
func (pq *PriorityQueue) Push(x interface{}) {
	*pq = append(*pq, x.(*Request))
}
func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[:n-1]
	return item
}

type Server struct {
	mu             sync.Mutex
	RequestQueue   PriorityQueue
	HasToken       bool
	LastHolder     int
	LastUsedTime   time.Time
	ClientFailures map[int]time.Time
	ActiveClients  map[int]bool
}

func NewServer() *Server {
	s := &Server{
		HasToken:       true,
		LastHolder:     -1,
		LastUsedTime:   time.Now(),
		ClientFailures: make(map[int]time.Time),
		ActiveClients:  make(map[int]bool),
	}
	heap.Init(&s.RequestQueue)
	go s.monitorIdleToken()
	go s.monitorClientFailures()
	go s.enforceFairness()
	return s
}

func (s *Server) RequestAccess(req Request, reply *bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	req.RequestTime = time.Now()
	heap.Push(&s.RequestQueue, &req)
	s.ActiveClients[req.ProcessID] = true

	if s.HasToken && s.LastHolder == -1 {
		s.grantToken()
	}

	*reply = true
	return nil
}

func (s *Server) ReleaseAccess(procID int, reply *bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fmt.Printf("Client %d released CS\n", procID)
	s.HasToken = true
	s.LastHolder = -1
	s.LastUsedTime = time.Now()
	s.grantToken()

	*reply = true
	return nil
}

func (s *Server) grantToken() {
	if s.RequestQueue.Len() > 0 && s.HasToken && s.LastHolder == -1 {
		req := heap.Pop(&s.RequestQueue).(*Request)
		s.HasToken = false
		s.LastHolder = req.ProcessID
		s.LastUsedTime = time.Now()
		fmt.Printf("Server: Granting token to Client %d (SeqNum %d, Waited %.1fs)\n",
			req.ProcessID, req.SeqNum, time.Since(req.RequestTime).Seconds())
		go s.watchTokenTimeout(req.ProcessID)
	}
}

func (s *Server) watchTokenTimeout(procID int) {
	time.Sleep(TokenTimeout)

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.HasToken && s.LastHolder == procID {
		fmt.Printf("Server: Client %d timeout. Revoking token.\n", procID)
		s.HasToken = true
		s.LastHolder = -1
		s.grantToken()
	}
}

func (s *Server) enforceFairness() {
	for {
		time.Sleep(5 * time.Second)
		s.mu.Lock()
		if s.RequestQueue.Len() > 0 {
			oldestReq := s.RequestQueue[0]
			if time.Since(oldestReq.RequestTime) > 15*time.Second {
				fmt.Printf("FAIRNESS: Client %d waiting too long (%.0fs), prioritizing\n",
					oldestReq.ProcessID, time.Since(oldestReq.RequestTime).Seconds())
				heap.Fix(&s.RequestQueue, 0)
			}
		}
		s.mu.Unlock()
	}
}

func (s *Server) monitorIdleToken() {
	for {
		time.Sleep(IdleCheckPeriod)
		s.mu.Lock()
		if s.HasToken && time.Since(s.LastUsedTime) > IdleCheckPeriod {
			fmt.Println("Server: Token idle for too long. Resetting.")
			s.LastHolder = -1
		}
		s.mu.Unlock()
	}
}

func (s *Server) monitorClientFailures() {
	for {
		time.Sleep(FailureTimeout / 2)
		s.mu.Lock()
		for clientID, lastSeen := range s.ClientFailures {
			if time.Since(lastSeen) > FailureTimeout && s.ActiveClients[clientID] {
				fmt.Printf("Server: Client %d assumed failed. Cleaning up.\n", clientID)
				delete(s.ClientFailures, clientID)
				delete(s.ActiveClients, clientID)
				if s.LastHolder == clientID {
					s.HasToken = true
					s.LastHolder = -1
					s.grantToken()
				}
			}
		}
		s.mu.Unlock()
	}
}

func (s *Server) HandleClientHeartbeat(procID int, reply *bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ClientFailures[procID] = time.Now()
	*reply = true
	return nil
}

func (s *Server) ClientExiting(procID int, reply *bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.ClientFailures, procID)
	delete(s.ActiveClients, procID)
	fmt.Printf("Server: Client %d exited gracefully\n", procID)
	*reply = true
	return nil
}

func main() {
	server := NewServer()
	rpc.Register(server)

	listener, err := net.Listen("tcp", ":1234")
	if err != nil {
		log.Fatal("Server error:", err)
	}
	defer listener.Close()

	fmt.Println("Server running on port 1234...")
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Connection error:", err)
			continue
		}
		go rpc.ServeConn(conn)
	}
}
