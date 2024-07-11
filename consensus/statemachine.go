package consensus

import (
	"encoding/json"
	"errors"
	"github.com/danthegoodman1/EpicEpoch/gologger"
	"github.com/lni/dragonboat/v3/statemachine"
	"io"
	"os"
)

type (
	EpochStateMachine struct {
		ClusterID uint64
		NodeID    uint64
		EpochFile string
		epoch     PersistenceEpoch
	}

	PersistenceEpoch struct {
		RaftIndex uint64
		Epoch     uint64
	}
)

var (
	logger = gologger.NewLogger()
)

func NewEpochStateMachine(clusterID, nodeID uint64) statemachine.IOnDiskStateMachine {
	epochFile := "./epoch.json" // TODO make this configurable and not json

	sm := &EpochStateMachine{
		ClusterID: clusterID,
		NodeID:    nodeID,
		EpochFile: epochFile,
	}

	return sm
}

func (e *EpochStateMachine) Open(stopc <-chan struct{}) (uint64, error) {
	// Read the current epoch now, crash if we can't
	if _, err := os.Stat(e.EpochFile); errors.Is(err, os.ErrNotExist) {
		e.epoch = PersistenceEpoch{
			RaftIndex: 0,
			Epoch:     0,
		}
	} else if err != nil {
		logger.Fatal().Err(err).Msg("error opening persistence file")
	} else {
		fileBytes, err := os.ReadFile(e.EpochFile)
		if err != nil {
			logger.Fatal().Err(err).Msg("error reading persistence file")
		}
		// TODO switch from json to proto when done dev
		err = json.Unmarshal(fileBytes, &e.epoch)
		if err != nil {
			logger.Fatal().Err(err).Msg("error deserializing persistence file, is it corrupted?")
		}
	}

	return e.epoch.RaftIndex, nil
}

func (e *EpochStateMachine) Update(entries []statemachine.Entry) ([]statemachine.Entry, error) {
	// TODO implement me
	panic("implement me")
}

func (e *EpochStateMachine) Lookup(i interface{}) (interface{}, error) {
	// TODO implement me
	panic("implement me")
}

func (e *EpochStateMachine) Sync() error {
	// TODO implement me
	panic("implement me")
}

func (e *EpochStateMachine) PrepareSnapshot() (interface{}, error) {
	// TODO implement me
	panic("implement me")
}

func (e *EpochStateMachine) SaveSnapshot(i interface{}, writer io.Writer, i2 <-chan struct{}) error {
	// TODO implement me
	panic("implement me")
}

func (e *EpochStateMachine) RecoverFromSnapshot(reader io.Reader, i <-chan struct{}) error {
	// TODO implement me
	panic("implement me")
}

func (e *EpochStateMachine) Close() error {
	// TODO implement me
	panic("implement me")
}
