package raft

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/danthegoodman1/EpicEpoch/ring"
	"github.com/danthegoodman1/EpicEpoch/utils"
	"github.com/lni/dragonboat/v3"
	"sync/atomic"
	"time"
)

type (
	EpochHost struct {
		nodeHost *dragonboat.NodeHost

		// The monotonic incrementing index of a single epoch.
		// Each request must be servied a unique (lastEpoch, epochIndex) value
		epochIndex atomic.Uint64

		// lastEpoch was the last epoch that we served to a request,
		// used to check whether we need to swap the epoch index
		lastEpoch atomic.Uint64

		readerAgentStopChan chan struct{}

		requestBuffer *ring.RingBuffer[*pendingRead]

		readerAgentReading atomic.Bool

		// pokeChan is used to poke the reader to generate timestamps
		pokeChan chan struct{}

		updateTicker *time.Ticker
	}

	pendingRead struct {
		// callbackRing is a ring buffer to write back to with the produced timestamp
		callbackRing *ring.RingBuffer[[]byte]
	}
)

// readerAgentLoop should be launched in a goroutine
func (e *EpochHost) readerAgentLoop() {
	for {
		logger.Debug().Msg("reader agent waiting...")
		select {
		case <-e.readerAgentStopChan:
			logger.Warn().Msg("reader agent loop received on stop chan, stopping")
			return
		case <-e.pokeChan:
			logger.Debug().Msg("reader agent poked")
			e.generateTimestamps()
		}
	}
}

// generateTimestamps generates timestamps for pending requests, and handles looping if more requests come in
func (e *EpochHost) generateTimestamps() {
	// Capture the current pending requests so there's no case we get locked
	pendingRequests := e.requestBuffer.Len()

	// Read the epoch
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*time.Duration(raftRttMs)*100)
	defer cancel()
	currentEpochI, err := e.nodeHost.SyncRead(ctx, ClusterID, nil)
	if err != nil {
		// This is never good, crash
		logger.Fatal().Err(err).Msg("error in nodeHost.SyncRead")
		return
	}

	currentEpoch, ok := currentEpochI.(PersistenceEpoch)
	if !ok {
		// This is never good, crash
		logger.Fatal().Err(err).Bool("ok", ok).Interface("currentEpoch", currentEpoch).Msg("lookup did not return a valid current epoch")
		return
	}

	if currentEpoch.Epoch == 0 {
		// Must be new, write one first
		logger.Warn().Msg("read current epoch 0, writing a value first")
		err = e.proposeNewEpoch(uint64(time.Now().UnixNano()))
		if err != nil {
			// This is never good, crash
			logger.Fatal().Err(err).Msg("error in nodeHost.SyncPropose")
			return
		}
	}

	if currentEpoch.Epoch != e.lastEpoch.Load() {
		// We need to push it forward and reset the index
		e.lastEpoch.Store(currentEpoch.Epoch)
		e.epochIndex.Store(0)
	}

	for range pendingRequests {
		// Write to the pending requests
		req, err := e.requestBuffer.Poll(time.Millisecond) // some timeout (this is quite long for a ring buffer)
		if err != nil {
			// This means that we lost items in the buffer somehow, very bad, must crash
			logger.Fatal().Err(err).Msg("timed out polling the ring buffer, did items get lost in the ring buffer?")
			return
		}

		reqIndex := e.epochIndex.Add(1)
		// Build the timestamp
		timestamp := make([]byte, 16) // 8 for epoch, 8 for index
		binary.BigEndian.PutUint64(timestamp[:8], currentEpoch.Epoch)
		binary.BigEndian.PutUint64(timestamp[8:], reqIndex)

		req.callbackRing.Put(timestamp)
	}

	if e.requestBuffer.Len() > 0 {
		// There are more requests, generating more timestamps
		logger.Debug().Msg("found more requests in ring buffer, generating more timestamps")
		e.generateTimestamps()
	}
}

// GetLeader returns the leader node ID of the specified Raft cluster based
// on local node's knowledge. The returned boolean value indicates whether the
// leader information is available.
func (e *EpochHost) GetLeader() (uint64, bool, error) {
	return e.nodeHost.GetLeaderID(ClusterID)
}

func (e *EpochHost) Stop() {
	e.updateTicker.Stop()
	e.readerAgentStopChan <- struct{}{}
	e.nodeHost.Stop()
}

var ErrNoDeadline = errors.New("missing deadline in context")

// GetUniqueTimestamp gets a unique hybrid timestamp to serve to a client
func (e *EpochHost) GetUniqueTimestamp(ctx context.Context) ([]byte, error) {
	pr := &pendingRead{callbackRing: ring.NewRingBuffer[[]byte](1)}

	// Register entry in ring buffer
	e.requestBuffer.Put(pr)

	// Try to poke the reader goroutine
	select {
	case e.pokeChan <- struct{}{}:
		logger.Debug().Msg("poked reader agent")
	default:
		logger.Debug().Msg("poke chan had nothing waiting")
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, ErrNoDeadline
	}

	// Wait for the response...
	timestamp, err := pr.callbackRing.Poll(deadline.Sub(time.Now()))
	if err != nil {
		return nil, fmt.Errorf("error in callbackRing.Get(): %w", err)
	}

	return timestamp, nil
}

func (e *EpochHost) proposeNewEpoch(newEpoch uint64) error {
	session := e.nodeHost.GetNoOPSession(ClusterID)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*time.Duration(raftRttMs)*200)
	defer cancel()
	_, err := e.nodeHost.SyncPropose(ctx, session, utils.MustMarshal(PersistenceEpoch{Epoch: newEpoch}))
	if err != nil {
		return fmt.Errorf("error in nodeHost.SyncPropose: %w", err)
	}

	return nil
}
