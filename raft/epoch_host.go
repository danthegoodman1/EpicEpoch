package raft

import (
	"context"
	"github.com/lni/dragonboat/v3"
	"sync/atomic"
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

		// timestampRequestChan is a buffered channel for incoming requests
		timestampRequestChan chan chan []byte
	}
)

// readerAgentLoop should be launched in a goroutine
func (e *EpochHost) readerAgentLoop() {
	for {
		func() { // wrapper for defer
			select {
			case <-e.readerAgentStopChan:
				logger.Warn().Msg("reader agent loop received on stop chan, stopping")
				return
			}
		}()
	}
}

// GetLeader returns the leader node ID of the specified Raft cluster based
// on local node's knowledge. The returned boolean value indicates whether the
// leader information is available.
func (e *EpochHost) GetLeader() (uint64, bool, error) {
	return e.nodeHost.GetLeaderID(ClusterID)
}

func (e *EpochHost) Stop() {
	e.nodeHost.Stop()
}

// GetUniqueTimestamp gets a unique hybrid timestamp to serve to a client
func (e *EpochHost) GetUniqueTimestamp(ctx context.Context) ([]byte, error) {
	// TODO register entry in ringbuffer
	// TODO if request not in flight
}
