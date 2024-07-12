package raft

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/danthegoodman1/EpicEpoch/gologger"
	"github.com/danthegoodman1/EpicEpoch/utils"
	"github.com/lni/dragonboat/v3/statemachine"
	"github.com/rs/zerolog"
	"io"
	"os"
)

// TODO replace JSON serialization to disk and network with protobuf?

type (
	EpochStateMachine struct {
		ClusterID uint64
		NodeID    uint64
		EpochFile string
		epoch     PersistenceEpoch
		closed    bool
		logger    zerolog.Logger
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
	epochFile := fmt.Sprintf("./epoch-%d.json", nodeID) // TODO make this configurable

	sm := &EpochStateMachine{
		ClusterID: clusterID,
		NodeID:    nodeID,
		EpochFile: epochFile,
		logger:    gologger.NewLogger(),
	}

	return sm
}

func (e *EpochStateMachine) Open(stopChan <-chan struct{}) (uint64, error) {
	e.logger.Debug().Msg("open")
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

		err = json.Unmarshal(fileBytes, &e.epoch)
		if err != nil {
			logger.Fatal().Err(err).Msg("error deserializing persistence file, is it corrupted?")
		}
	}

	return e.epoch.RaftIndex, nil
}

func (e *EpochStateMachine) Update(entries []statemachine.Entry) ([]statemachine.Entry, error) {
	e.logger.Debug().Interface("entries", entries).Msg("update")
	if e.closed {
		panic("Update called after close!")
	}

	// Since all writes are to the same key, we can just take the last one and write it
	// Update it in memory
	// Verify that it's newer than the current time
	var newEpoch PersistenceEpoch
	err := json.Unmarshal(entries[len(entries)-1].Cmd, &newEpoch)
	if err != nil {
		return nil, fmt.Errorf("error in json.Unmarshal: %w", err)
	}

	if newEpoch.Epoch <= e.epoch.Epoch {
		return nil, fmt.Errorf("update epoch was not greater than the current epoch, there is clock drift or a bug")
	}
	// Otherwise we can update it
	e.epoch = newEpoch

	e.epoch.RaftIndex = entries[len(entries)-1].Index

	err = WriteFileAtomic(e.EpochFile, utils.MustMarshal(e.epoch), 0777)
	if err != nil {
		return nil, fmt.Errorf("error writing atomically to file %s: %w", e.EpochFile, err)
	}

	// Just return the entries, there isn't a status code here, it either worked or exploded
	return entries, nil
}

var ErrAlreadyClosed = errors.New("already closed")

func (e *EpochStateMachine) Lookup(i interface{}) (interface{}, error) {
	e.logger.Debug().Interface("lookup", i).Msg("lookup")
	if e.closed {
		return nil, ErrAlreadyClosed
	}

	// Only need to return the current epoch
	return e.epoch, nil
}

func (e *EpochStateMachine) Sync() error {
	e.logger.Debug().Msg("sync")
	if e.closed {
		panic("Sync called after close!")
	}
	// Because we write atomically in Update, we do not need to do anything here
	return nil
}

func (e *EpochStateMachine) PrepareSnapshot() (interface{}, error) {
	e.logger.Debug().Msg("prepare snapshot")
	if e.closed {
		panic("PrepareSnapshot called after close!")
	}

	// Need to save a serialization of the state
	return utils.MustMarshal(e.epoch), nil
}

func (e *EpochStateMachine) SaveSnapshot(i interface{}, writer io.Writer, stopChan <-chan struct{}) error {
	e.logger.Debug().Msg("save snapshot")
	if e.closed {
		panic("SaveSnapshot called after close!")
	}

	serializedEpoch, ok := i.([]byte)
	if !ok {
		return fmt.Errorf("prepared snapshot was not bytes")
	}

	_, err := writer.Write(serializedEpoch)
	if err != nil {
		return fmt.Errorf("error in writer.Write: %w", err)
	}

	return nil
}

func (e *EpochStateMachine) RecoverFromSnapshot(reader io.Reader, stopChan <-chan struct{}) error {
	e.logger.Debug().Msg("recover from snapshot")
	if e.closed {
		panic("RecoverFromSnapshot called after close!")
	}

	serializedEpoch, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("error in io.ReadAll: %w", err)
	}

	// First, save to memory to make sure it is good
	err = json.Unmarshal(serializedEpoch, &e.epoch)
	if err != nil {
		return fmt.Errorf("error in json.Unmarshal: %w", err)
	}

	// Then write it to disk
	err = WriteFileAtomic(e.EpochFile, serializedEpoch, 0777)
	if err != nil {
		return fmt.Errorf("error in WriteFileAtomic: %w", err)
	}

	return nil
}

func (e *EpochStateMachine) Close() error {
	e.logger.Debug().Msg("close")
	// Dragonboat claims that nothing else besides Lookup may be called after closed,
	// but we can also be safe :)
	if e.closed {
		panic("already closed state machine!")
	}
	e.closed = true
	return nil
}
