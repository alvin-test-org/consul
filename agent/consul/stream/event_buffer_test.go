package stream

import (
	"context"
	fmt "fmt"
	"math/rand"
	"testing"
	time "time"

	"github.com/stretchr/testify/assert"
	
	"github.com/hashicorp/consul/agent/agentpb"
)

func TestEventBufferFuzz(t *testing.T) {
	// A property-based test to ensure that under heavy concurrent use trivial
	// correctness properties are not violated (and that -race doesn't complain).

	nReaders := 1000
	nMessages := 1000

	b := NewEventBuffer()

	// Start a write goroutine that will publish 10000 messages with sequential
	// indexes and some jitter in timing (to allow clients to "catch up" and block
	// waiting for updates).
	go func() {
		// z is a Zipfian distribution that gives us a number of milliseconds to
		// sleep which are mostly low - near zero but occasionally spike up to near
		// 100.
		z := rand.NewZipf(rand.New(rand.NewSource(1)), 1.5, 1.5, 50)

		for i := 0; i < nMessages; i++ {
			// Event content is arbitrary and not valid for our use of buffers in
			// streaming - here we only care about the semantics of the buffer.
			e := agentpb.Event{
				Index:   uint64(i), // Indexes should be contiguous
				Topic:   agentpb.Topic_ServiceHealth,
				Payload: &agentpb.Event_EndOfSnapshot{
					EndOfSnapshot: true,
				},
			}
			b.Append([]agentpb.Event{e})
			// Sleep sometimes for a while to let some subscribers catch up
			wait := time.Duration(z.Uint64()) * time.Millisecond
			time.Sleep(wait)
		}
	}()

	// Run n subscribers following and verifying
	errCh := make(chan error, nReaders)

	// Load head here so all subscribers start from the same point or they might
	// no run until several appends have already happened.
	head := b.Head()

	for i := 0; i < nReaders; i++ {
		go func(i int) {
			expect := uint64(0)
			item := head
			var err error
			for {
				item, err = item.Next(context.Background())
				if err != nil {
					errCh <- fmt.Errorf("subscriber %05d failed getting next %d: %s", i,
						expect, err)
					return
				}
				if item.Events[0].Index != expect {
					errCh <- fmt.Errorf("subscriber %05d got bad event want=%d, got=%d", i,
						expect, item.Events[0].Index)
					return
				}
				expect++
				if expect == uint64(nMessages) {
					// Succeeded
					errCh <- nil
					return
				}
			}
		}(i)
	}

	// Wait for all readers to finish one way or other
	for i := 0; i < nReaders; i++ {
		err := <-errCh
		assert.NoError(t, err)
	}
}
