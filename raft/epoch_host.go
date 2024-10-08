package raft

import (
	"context"
	"encoding/binary"
	"fmt"
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

		requestChan chan *pendingRead

		readerAgentReading atomic.Bool

		// pokeChan is used to poke the reader to generate timestamps
		pokeChan chan struct{}

		updateTicker *time.Ticker
	}

	pendingRead struct {
		// callbackChan is a channel to write back to with the produced timestamp
		callbackChan chan []byte
		count        int
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
	pendingRequests := len(e.requestChan)
	logger.Debug().Msgf("Serving %d pending requests", pendingRequests)

	// Read the epoch
	s := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*time.Duration(raftRttMs)*100)
	defer cancel()
	currentEpochI, err := e.nodeHost.SyncRead(ctx, ClusterID, nil)
	if err != nil {
		// This is never good, crash
		logger.Fatal().Err(err).Msg("error in nodeHost.SyncRead")
		return
	}
	logger.Debug().Msgf("Read from raft in %+v", time.Since(s))

	currentEpoch, ok := currentEpochI.(PersistenceEpoch)
	if !ok {
		// This is never good, crash
		logger.Fatal().Err(err).Bool("ok", ok).Interface("currentEpoch", currentEpoch).Msg("lookup did not return a valid current epoch")
		return
	}

	if currentEpoch.Epoch == 0 {
		// Must be new, write one first
		currentEpoch.Epoch = uint64(time.Now().UnixNano())
		logger.Warn().Msgf("read current epoch 0, writing first value %d", currentEpoch.Epoch)
		err = e.proposeNewEpoch(currentEpoch.Epoch)
		if err != nil {
			// This is never good, crash
			logger.Fatal().Err(err).Msg("error in nodeHost.SyncPropose")
			return
		}
	} else if e.lastEpoch.Load() == 0 {
		// We recently became the leader, we must increment the epoch
		currentEpoch.Epoch = uint64(time.Now().UnixNano())
		logger.Warn().Msgf("we must have been elected, incrementing epoch %d", currentEpoch.Epoch)
		if currentEpoch.Epoch <= e.lastEpoch.Load() {
			logger.Error().Uint64("newEpoch", currentEpoch.Epoch).Uint64("lastEpoch", e.lastEpoch.Load()).Msg("new epoch less than last epoch, there must be clock drift, incrementing new epoch by 1")
			currentEpoch.Epoch++
		}
		err = e.proposeNewEpoch(currentEpoch.Epoch)
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

	s = time.Now()
	for range pendingRequests {
		// Write to the pending requests
		req := <-e.requestChan

		// Build the timestamp
		timestamp := make([]byte, 16*req.count) // 8 for epoch, 8 for index, multiply for each
		logger.Debug().Msgf("writing for %d", req.count)
		for i := 0; i < req.count; i++ {
			reqIndex := e.epochIndex.Add(1)
			offset := i * 16
			fmt.Println("writing to bounds", offset, offset+16)
			binary.BigEndian.PutUint64(timestamp[offset:offset+8], currentEpoch.Epoch)
			binary.BigEndian.PutUint64(timestamp[offset+8:offset+16], reqIndex)
		}

		select {
		case req.callbackChan <- timestamp:
		default:
			logger.Warn().Msg("did not have listener on callback chan when generating timestamp")
		}
	}
	logger.Debug().Msgf("Served %d requests in %+v", pendingRequests, time.Since(s))

	if len(e.requestChan) > 0 {
		// There are more requests, generating more timestamps
		logger.Debug().Msg("found more requests in request channel, generating more timestamps")
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

// GetUniqueTimestamp gets a unique hybrid timestamp to serve to a client
func (e *EpochHost) GetUniqueTimestamp(ctx context.Context, count int) ([]byte, error) {
	if count < 1 {
		return nil, fmt.Errorf("count must be >= 1")
	}
	pr := &pendingRead{callbackChan: make(chan []byte, 1), count: count}

	// Register request
	err := utils.WriteWithContext(ctx, e.requestChan, pr)
	if err != nil {
		return nil, fmt.Errorf("error writing nil, pending request to request buffer: %w", err)
	}

	// Try to poke the reader goroutine
	select {
	case e.pokeChan <- struct{}{}:
		logger.Debug().Msg("poked reader agent")
	default:
		logger.Debug().Msg("poke chan had nothing waiting")
	}

	// Wait for the response
	timestamp, err := utils.ReadWithContext(ctx, pr.callbackChan)
	if err != nil {
		return nil, fmt.Errorf("error reading from callback channel with context: %w", err)
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

type (
	Membership struct {
		Leader  Member   `json:"leader"`
		Members []Member `json:"members"`
	}

	Member struct {
		NodeID uint64 `json:"nodeID"`
		Addr   string `json:"addr"`
	}
)

func (e *EpochHost) GetMembership(ctx context.Context) (*Membership, error) {
	leader, available, err := e.nodeHost.GetLeaderID(ClusterID)
	if err != nil {
		return nil, fmt.Errorf("error getting membership: %e", err)
	}

	if !available {
		return nil, fmt.Errorf("raft membership not avilable")
	}

	membership, err := e.nodeHost.SyncGetClusterMembership(ctx, ClusterID)
	if err != nil {
		return nil, fmt.Errorf("error in nodeHost.SyncGetClusterMembership: %w", err)
	}

	m := &Membership{}
	for id, node := range membership.Nodes {
		if id == leader {
			m.Leader = Member{
				NodeID: id,
				Addr:   node,
			}
		}
		m.Members = append(m.Members, Member{
			NodeID: id,
			Addr:   node,
		})
	}

	return m, nil
}
