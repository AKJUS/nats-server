// Copyright 2018-2025 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build !race && !skipnoracetests

package test

import (
	"fmt"
	"strconv"
	"sync"
	"testing"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// IMPORTANT: Tests in this file are not executed when running with the -race flag.
//            The test name should be prefixed with TestNoRace so we can run only
//            those tests: go test -run=TestNoRace ...

// As we look to improve high fanout situations make sure we
// have a test that checks ordering for all subscriptions from a single subscriber.
func TestNoRaceHighFanoutOrdering(t *testing.T) {
	opts := &server.Options{Host: "127.0.0.1", Port: server.RANDOM_PORT}

	s := RunServer(opts)
	defer s.Shutdown()

	url := fmt.Sprintf("nats://%s", s.Addr())

	const (
		nconns = 100
		nsubs  = 100
		npubs  = 500
	)

	// make unique
	subj := nats.NewInbox()

	var wg sync.WaitGroup
	wg.Add(nconns * nsubs)

	for i := 0; i < nconns; i++ {
		nc, err := nats.Connect(url)
		if err != nil {
			t.Fatalf("Expected a successful connect on %d, got %v\n", i, err)
		}

		nc.SetErrorHandler(func(c *nats.Conn, s *nats.Subscription, e error) {
			t.Fatalf("Got an error %v for %+v\n", s, err)
		})

		for y := 0; y < nsubs; y++ {
			expected := 0
			nc.Subscribe(subj, func(msg *nats.Msg) {
				n, _ := strconv.Atoi(string(msg.Data))
				if n != expected {
					t.Fatalf("Expected %d but received %d\n", expected, n)
				}
				expected++
				if expected >= npubs {
					wg.Done()
				}
			})
		}
		nc.Flush()
		defer nc.Close()
	}

	nc, _ := nats.Connect(url)

	for i := 0; i < npubs; i++ {
		nc.Publish(subj, []byte(strconv.Itoa(i)))
	}
	defer nc.Close()

	wg.Wait()
}

func TestNoRaceRouteFormTimeWithHighSubscriptions(t *testing.T) {
	srvA, optsA := RunServerWithConfig("./configs/srv_a.conf")
	defer srvA.Shutdown()

	clientA := createClientConn(t, optsA.Host, optsA.Port)
	defer clientA.Close()

	sendA, expectA := setupConn(t, clientA)

	// Now add lots of subscriptions. These will need to be forwarded
	// to new routes when they are added.
	subsTotal := 100000
	for i := 0; i < subsTotal; i++ {
		subject := fmt.Sprintf("FOO.BAR.BAZ.%d", i)
		sendA(fmt.Sprintf("SUB %s %d\r\n", subject, i))
	}
	sendA("PING\r\n")
	expectA(pongRe)

	srvB, _ := RunServerWithConfig("./configs/srv_b.conf")
	defer srvB.Shutdown()

	checkClusterFormed(t, srvA, srvB)

	// Now wait for all subscriptions to be processed.
	if err := checkExpectedSubs(subsTotal, srvB); err != nil {
		// Make sure we are not a slow consumer
		// Check for slow consumer status
		if srvA.NumSlowConsumers() > 0 {
			t.Fatal("Did not receive all subscriptions due to slow consumer")
		} else {
			t.Fatalf("%v", err)
		}
	}
	// Just double check the slow consumer status.
	if srvA.NumSlowConsumers() > 0 {
		t.Fatalf("Received a slow consumer notification: %d", srvA.NumSlowConsumers())
	}
}
