package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/websocket"
)

type FirehoseMessage struct {
	Seq       int64  `json:"seq"`
	Type      string `json:"type"`
	DID       string `json:"did"`
	CommitCID string `json:"commit_cid,omitempty"`
	Time      string `json:"time"`
	Host      string `json:"host"`
}

func main() {
	url := "ws://127.0.0.1:2470/xrpc/com.atproto.sync.subscribeRepos"
	if len(os.Args) > 1 {
		url = os.Args[1]
	}

	fmt.Printf("Connecting to %s...\n", url)

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer conn.Close()

	fmt.Println("Connected! Waiting for events...")

	// Handle Ctrl+C
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	done := make(chan struct{})
	count := 0
	var lastSeq int64
	var seqErrors []string

	go func() {
		defer close(done)
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				log.Println("read:", err)
				return
			}
			count++

			// Parse the message to check sequence
			var msg FirehoseMessage
			if err := json.Unmarshal(message, &msg); err == nil {
				// Check sequence ordering
				if lastSeq > 0 && msg.Seq != lastSeq+1 && msg.Seq != 0 {
					seqErrors = append(seqErrors, fmt.Sprintf("Gap: expected %d, got %d", lastSeq+1, msg.Seq))
				}
				if msg.Seq > 0 {
					lastSeq = msg.Seq
				}

				if count <= 10 || count%100 == 0 {
					fmt.Printf("[%d] seq=%d type=%s did=%s\n", count, msg.Seq, msg.Type, msg.DID)
				}
			} else {
				fmt.Printf("[%d] %s\n", count, string(message))
			}
		}
	}()

	select {
	case <-done:
	case <-interrupt:
		fmt.Printf("\nReceived %d events, last seq: %d\n", count, lastSeq)
	case <-time.After(10 * time.Second):
		fmt.Printf("\nTimeout - received %d events, last seq: %d\n", count, lastSeq)
	}

	if len(seqErrors) > 0 {
		fmt.Println("\nSequence errors detected:")
		for _, e := range seqErrors {
			fmt.Println("  -", e)
		}
	} else if count > 0 {
		fmt.Println("\nNo sequence errors - all events in order!")
	}
}
