package oe

import (
	"bytes"
	"context"
	"encoding/hex"
	"math/rand"
	"sync"
	"time"

	abci "github.com/cometbft/cometbft/api/cometbft/abci/v1"

	"cosmossdk.io/core/server"
	"cosmossdk.io/core/store"
	"cosmossdk.io/core/transaction"
	"cosmossdk.io/log"
)

// FinalizeBlockFunc is the function that is called by the OE to finalize the
// block. It is the same as the one in the ABCI app.
type FinalizeBlockFunc[T transaction.Tx] func(context.Context, *abci.FinalizeBlockRequest) (*server.BlockResponse, store.WriterMap, []T, error)

// OptimisticExecution is a struct that contains the OE context. It is used to
// run the FinalizeBlock function in a goroutine, and to abort it if needed.
type OptimisticExecution[T transaction.Tx] struct {
	finalizeBlockFunc FinalizeBlockFunc[T] // ABCI FinalizeBlock function with a context
	logger            log.Logger

	mtx         sync.Mutex
	stopCh      chan struct{}
	request     *abci.FinalizeBlockRequest
	response    *FinalizeBlockResponse[T]
	err         error
	cancelFunc  func() // cancel function for the context
	initialized bool   // A boolean value indicating whether the struct has been initialized

	// debugging/testing options
	abortRate int // number from 0 to 100 that determines the percentage of OE that should be aborted
}

type FinalizeBlockResponse[T transaction.Tx] struct {
	Resp         *server.BlockResponse
	StateChanges store.WriterMap
	DecodedTxs   []T
}

// NewOptimisticExecution initializes the Optimistic Execution context but does not start it.
func NewOptimisticExecution[T transaction.Tx](logger log.Logger, fn FinalizeBlockFunc[T], opts ...func(*OptimisticExecution[T])) *OptimisticExecution[T] {
	logger = logger.With(log.ModuleKey, "oe")
	oe := &OptimisticExecution[T]{logger: logger, finalizeBlockFunc: fn}
	for _, opt := range opts {
		opt(oe)
	}
	return oe
}

// WithAbortRate sets the abort rate for the OE. The abort rate is a number from
// 0 to 100 that determines the percentage of OE that should be aborted.
// This is for testing purposes only and must not be used in production.
func WithAbortRate[T transaction.Tx](rate int) func(*OptimisticExecution[T]) {
	return func(oe *OptimisticExecution[T]) {
		oe.abortRate = rate
	}
}

// Reset resets the OE context. Must be called whenever we want to invalidate
// the current OE.
func (oe *OptimisticExecution[T]) Reset() {
	oe.mtx.Lock()
	defer oe.mtx.Unlock()
	oe.request = nil
	oe.response = nil
	oe.err = nil
	oe.initialized = false
}

// Initialized returns true if the OE was initialized, meaning that it contains
// a request and it was run or it is running.
func (oe *OptimisticExecution[T]) Initialized() bool {
	if oe == nil {
		return false
	}
	oe.mtx.Lock()
	defer oe.mtx.Unlock()

	return oe.initialized
}

// Execute initializes the OE and starts it in a goroutine.
func (oe *OptimisticExecution[T]) Execute(req *abci.ProcessProposalRequest) {
	oe.mtx.Lock()
	defer oe.mtx.Unlock()

	oe.stopCh = make(chan struct{})
	oe.request = &abci.FinalizeBlockRequest{
		Txs:                req.Txs,
		DecidedLastCommit:  req.ProposedLastCommit,
		Misbehavior:        req.Misbehavior,
		Hash:               req.Hash,
		Height:             req.Height,
		Time:               req.Time,
		NextValidatorsHash: req.NextValidatorsHash,
		ProposerAddress:    req.ProposerAddress,
	}

	oe.logger.Debug("OE started", "height", req.Height, "hash", hex.EncodeToString(req.Hash), "time", req.Time.String())
	ctx, cancel := context.WithCancel(context.Background())
	oe.cancelFunc = cancel
	oe.initialized = true

	go func() {
		start := time.Now()
		resp, stateChanges, decodedTxs, err := oe.finalizeBlockFunc(ctx, oe.request)

		oe.mtx.Lock()

		executionTime := time.Since(start)
		oe.logger.Debug("OE finished", "duration", executionTime.String(), "height", oe.request.Height, "hash", hex.EncodeToString(oe.request.Hash))
		oe.response, oe.err = &FinalizeBlockResponse[T]{
			Resp:         resp,
			StateChanges: stateChanges,
			DecodedTxs:   decodedTxs,
		}, err

		close(oe.stopCh)
		oe.mtx.Unlock()
	}()
}

// AbortIfNeeded aborts the OE if the request hash is not the same as the one in
// the running OE. Returns true if the OE was aborted.
func (oe *OptimisticExecution[T]) AbortIfNeeded(reqHash []byte) bool {
	if oe == nil {
		return false
	}

	oe.mtx.Lock()
	defer oe.mtx.Unlock()

	if !bytes.Equal(oe.request.Hash, reqHash) {
		oe.logger.Error("OE aborted due to hash mismatch", "oe_hash", hex.EncodeToString(oe.request.Hash), "req_hash", hex.EncodeToString(reqHash), "oe_height", oe.request.Height, "req_height", oe.request.Height)
		oe.cancelFunc()
		return true
	} else if oe.abortRate > 0 && rand.Intn(100) < oe.abortRate {
		// this is for test purposes only, we can emulate a certain percentage of
		// OE needed to be aborted.
		oe.cancelFunc()
		oe.logger.Error("OE aborted due to test abort rate")
		return true
	}

	return false
}

// Abort aborts the OE unconditionally and waits for it to finish.
func (oe *OptimisticExecution[T]) Abort() {
	if oe == nil || oe.cancelFunc == nil {
		return
	}

	oe.cancelFunc()
	<-oe.stopCh
}

// WaitResult waits for the OE to finish and returns the result.
func (oe *OptimisticExecution[T]) WaitResult() (*FinalizeBlockResponse[T], error) {
	<-oe.stopCh
	return oe.response, oe.err
}