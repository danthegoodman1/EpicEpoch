package raft

import (
	"context"
	"errors"
	"fmt"
	"github.com/danthegoodman1/EpicEpoch/ring"
	"github.com/danthegoodman1/EpicEpoch/utils"
	"github.com/lni/dragonboat/v3"
	"github.com/lni/dragonboat/v3/config"
	dragonlogger "github.com/lni/dragonboat/v3/logger"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

const ClusterID = 100

var raftRttMs = uint64(utils.GetEnvOrDefaultInt("RTT_MS", 10))

func StartRaft() (*EpochHost, error) {
	nodeID := utils.NodeID
	rc := config.Config{
		NodeID:             nodeID,
		ClusterID:          ClusterID,
		ElectionRTT:        10,
		HeartbeatRTT:       1,
		CheckQuorum:        true,
		SnapshotEntries:    10,
		CompactionOverhead: 5,
	}
	datadir := filepath.Join("_raft", fmt.Sprintf("node%d", nodeID))
	nhc := config.NodeHostConfig{
		WALDir:         datadir,
		NodeHostDir:    datadir,
		RTTMillisecond: raftRttMs,
		RaftAddress:    os.Getenv("RAFT_ADDR"),
	}
	dragonlogger.SetLoggerFactory(CreateLogger)
	nh, err := dragonboat.NewNodeHost(nhc)
	if err != nil {
		panic(err)
	}

	err = nh.StartOnDiskCluster(map[uint64]dragonboat.Target{
		1: "localhost:60001",
		2: "localhost:60002",
		3: "localhost:60003",
	}, false, NewEpochStateMachine, rc)
	if err != nil {
		return nil, fmt.Errorf("error in StartOnDiskCluster: %w", err)
	}

	eh := &EpochHost{
		nodeHost:            nh,
		epochIndex:          atomic.Uint64{},
		lastEpoch:           atomic.Uint64{},
		readerAgentStopChan: make(chan struct{}),
		requestBuffer:       ring.NewRingBuffer[*pendingRead](utils.TimestampRequestBuffer),
		readerAgentReading:  atomic.Bool{},
		pokeChan:            make(chan struct{}),
		updateTicker:        time.NewTicker(time.Millisecond * time.Duration(utils.EpochIntervalMS)),
	}
	eh.epochIndex.Store(0)
	eh.lastEpoch.Store(0)
	eh.readerAgentReading.Store(false)

	// Debug log loop
	go func() {
		warnedClockDrift := false // dedupe for logging clock drift
		deadlines := int64(0)     // propose deadlines counter to eventually crash
		for {
			_, ok := <-eh.updateTicker.C
			if !ok {
				logger.Warn().Msg("ticker channel closed, returning")
				return
			}
			leader, available, err := nh.GetLeaderID(ClusterID)
			if err != nil {
				logger.Fatal().Err(err).Msg("error getting leader id, crashing")
				return
			}
			// logger.Debug().Err(err).Msgf("Leader=%d available=%+v", leader, available)
			if available && leader == utils.NodeID {
				// Update the epoch
				newEpoch := uint64(time.Now().UnixNano())
				if newEpoch <= eh.lastEpoch.Load() && !warnedClockDrift {
					warnedClockDrift = true
					logger.Error().Uint64("newEpoch", newEpoch).Uint64("lastEpoch", eh.lastEpoch.Load()).Msg("new epoch less than last epoch, there must be clock drift, incrementing new epoch by 1")
					newEpoch++
				} else {
					warnedClockDrift = false
					// Write the new value
					err = eh.proposeNewEpoch(newEpoch)
					if errors.Is(err, context.DeadlineExceeded) {
						deadlines++
						logger.Error().Str("crashTreshold", fmt.Sprintf("%d/%d", deadlines, utils.EpochIntervalDeadlineLimit)).Msg("deadline exceeded proposing new epoch")
						if deadlines >= utils.EpochIntervalDeadlineLimit {
							logger.Fatal().Msg("new epoch deadline threshold exceeded, crashing")
							return
						}
					}
					if err != nil {
						// Bad, crash
						logger.Fatal().Err(err).Msg("error in proposeNewEpoch, crashing")
						return
					}

					deadlines = 0 // reset the deadlines counter
				}
			}
		}
	}()

	go eh.readerAgentLoop()

	return eh, nil
}
