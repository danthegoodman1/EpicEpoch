package raft

import (
	"context"
	"fmt"
	"github.com/danthegoodman1/EpicEpoch/utils"
	"github.com/lni/dragonboat/v3"
	"github.com/lni/dragonboat/v3/config"
	dragonlogger "github.com/lni/dragonboat/v3/logger"
	"os"
	"path/filepath"
	"time"
)

func StartRaft() (*dragonboat.NodeHost, error) {
	nodeID := uint64(utils.GetEnvOrDefaultInt("NODE_ID", 0))
	rc := config.Config{
		NodeID:             nodeID,
		ClusterID:          111,
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
		RTTMillisecond: 10, // TODO: make this configurable, start lower (1ms)
		RaftAddress:    os.Getenv("RAFT_ADDR"),
	}
	dragonlogger.SetLoggerFactory(CreateLogger)
	nh, err := dragonboat.NewNodeHost(nhc)
	if err != nil {
		panic(err)
	}

	go func() {
		t := time.NewTicker(time.Second * 3)
		for {
			<-t.C
			fmt.Println("LeaderID:")
			fmt.Println(nh.GetLeaderID(111))
			fmt.Println("")

			leader, _, _ := nh.GetLeaderID(111)
			if leader != nodeID && nodeID == 3 {
				// Propose something
				ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
				nh.SyncPropose(ctx, nh.GetNoOPSession(111), utils.MustMarshal(PersistenceEpoch{
					Epoch: uint64(time.Now().UnixNano()),
				}))
				cancel()
			}
			if leader == nodeID {
				// Let's read
				ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
				i, err := nh.SyncRead(ctx, 111, nil)
				if err != nil {
					panic(err)
				}
				logger.Info().Msgf("GOT READ VALUE: %+v", i)
				cancel()
			}
		}
	}()

	return nh, nh.StartOnDiskCluster(map[uint64]dragonboat.Target{
		1: "localhost:60001",
		2: "localhost:60002",
		3: "localhost:60003",
	}, false, NewEpochStateMachine, rc)
}
