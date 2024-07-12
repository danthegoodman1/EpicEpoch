package raft

import (
	"fmt"
	"github.com/danthegoodman1/EpicEpoch/gologger"
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
		RTTMillisecond: uint64(utils.GetEnvOrDefaultInt("RTT_MS", 10)),
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

	// Debug log loop
	go func() {
		logger := gologger.NewLogger()
		t := time.NewTicker(time.Second * 5)
		for {
			<-t.C
			leader, available, err := nh.GetLeaderID(ClusterID)
			logger.Debug().Err(err).Msgf("Leader=%d available=%+v", leader, available)
		}
	}()

	eh := &EpochHost{
		nodeHost:   nh,
		epochIndex: atomic.Uint64{},
		lastEpoch:  atomic.Uint64{},
	}

	return eh, nil
}
