package main

import (
	"fmt"
	"log"
	"math/rand"
	"net/rpc"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type Request struct {
	SeqNum      int
	ProcessID   int
	RequestTime time.Time
}

const (
	MaxRequests    = 10
	HeartbeatFreq  = 2 * time.Second
	BaseWorkTime   = 1 * time.Second
	MaxJitter      = 2 * time.Second
	RetryDelayBase = 500 * time.Millisecond
)

func main() {
	rand.Seed(time.Now().UnixNano())
	var wg sync.WaitGroup

	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			client(id)
		}(i)
		time.Sleep(time.Duration(rand.Intn(500)) * time.Millisecond)
	}

	wg.Wait()
	fmt.Println("All clients completed")
}

func client(id int) {
	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Connect with retries
	var client *rpc.Client
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		client, err = rpc.Dial("tcp", "localhost:1234")
		if err == nil {
			break
		}
		time.Sleep(RetryDelayBase * time.Duration(attempt+1))
	}
	if err != nil {
		log.Printf("Client %d: Connection failed after retries: %v", id, err)
		return
	}
	defer client.Close()

	// Heartbeat goroutine
	stopHeartbeat := make(chan struct{})
	go func() {
		ticker := time.NewTicker(HeartbeatFreq)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				var reply bool
				err := client.Call("Server.HandleClientHeartbeat", id, &reply)
				if err != nil {
					return
				}
			case <-stopHeartbeat:
				return
			}
		}
	}()

	// Cleanup on exit
	defer func() {
		close(stopHeartbeat)
		var reply bool
		client.Call("Server.ClientExiting", id, &reply)
	}()

	for seqNum := 1; seqNum <= MaxRequests; seqNum++ {
		select {
		case <-sigChan:
			log.Printf("Client %d: Received shutdown signal", id)
			return
		default:
			req := Request{
				SeqNum:    seqNum,
				ProcessID: id,
			}

			// Request with retries
			var granted bool
			for attempt := 0; attempt < 3; attempt++ {
				err := client.Call("Server.RequestAccess", req, &granted)
				if err == nil {
					break
				}
				time.Sleep(RetryDelayBase * time.Duration(attempt+1))
			}

			if granted {
				workTime := BaseWorkTime + time.Duration(rand.Int63n(MaxJitter.Nanoseconds()))
				fmt.Printf("Client %d entered CS (Req %d) for %.1fs\n",
					id, seqNum, workTime.Seconds())
				time.Sleep(workTime)

				// Release with retries
				for attempt := 0; attempt < 3; attempt++ {
					err := client.Call("Server.ReleaseAccess", id, &granted)
					if err == nil {
						fmt.Printf("Client %d released CS (Req %d)\n", id, seqNum)
						break
					}
					time.Sleep(RetryDelayBase * time.Duration(attempt+1))
				}
			}

			// Random delay between requests
			time.Sleep(time.Duration(rand.Intn(1000)) * time.Millisecond)
		}
	}
}
