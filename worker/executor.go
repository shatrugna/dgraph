/*
 * Copyright 2016-2020 Dgraph Labs, Inc. and Contributors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Package worker contains code for pb.worker communication to perform
// queries and mutations.
package worker

import (
	"context"
	"encoding/binary"
	"sync"
	"sync/atomic"

	"github.com/dgraph-io/badger/v2/y"
	"github.com/dgraph-io/dgraph/posting"
	"github.com/dgraph-io/dgraph/protos/pb"
	"github.com/dgraph-io/dgraph/x"
	"github.com/dgraph-io/ristretto/z"
	"github.com/golang/glog"
)

type subMutation struct {
	edges   []*pb.DirectedEdge
	ctx     context.Context
	startTs uint64
}

type executor struct {
	pendingSize int64

	sync.RWMutex
	workerChan []chan *subMutation
	closer     *y.Closer
}

func newExecutor() *executor {
	e := &executor{
		workerChan: make([]chan *subMutation, 32), /* TODO: no of chans can be made configurable */
		closer:     y.NewCloser(0),
	}

	for i := 0; i < len(e.workerChan); i++ {
		e.workerChan[i] = make(chan *subMutation, 1000)
		e.closer.AddRunning(1)
		go e.processWorkerCh(e.workerChan[i])
	}
	go e.shutdown()
	return e
}

func (e *executor) processWorkerCh(ch chan *subMutation) {
	defer e.closer.Done()

	writer := posting.NewTxnWriter(pstore)
	for payload := range ch {
		var esize int64
		ptxn := posting.NewTxn(payload.startTs)
		for _, edge := range payload.edges {
			esize += int64(edge.Size())
			for {
				err := runMutation(payload.ctx, edge, ptxn)
				if err == nil {
					break
				} else if err != posting.ErrRetry {
					glog.Errorf("Error while mutating: %v", err)
					break
				}
			}
		}
		ptxn.Update()
		if err := ptxn.CommitToDisk(writer, payload.startTs); err != nil {
			glog.Errorf("Error while commiting to disk: %v", err)
		}
		// TODO(Animesh): We might not need this wait.
		if err := writer.Wait(); err != nil {
			glog.Errorf("Error while waiting for writes: %v", err)
		}

		atomic.AddInt64(&e.pendingSize, -esize)
	}
}

func (e *executor) shutdown() {
	<-e.closer.HasBeenClosed()
	e.Lock()
	defer e.Unlock()
	for _, ch := range e.workerChan {
		close(ch)
	}
}

// channelID obtains the channel for the given edge.
func (e *executor) channelID(edge *pb.DirectedEdge) int {
	attr := edge.Attr
	uid := edge.Entity
	b := make([]byte, len(attr)+8)
	x.AssertTrue(len(attr) == copy(b, attr))
	binary.BigEndian.PutUint64(b[len(attr):len(attr)+8], uid)
	cid := z.MemHash(b) % uint64(len(e.workerChan))
	return int(cid)
}

const (
	maxPendingEdgesSize int64 = 64 << 20
	executorAddEdges          = "executor.addEdges"
)

func (e *executor) addEdges(ctx context.Context, startTs uint64, edges []*pb.DirectedEdge) {
	rampMeter(&e.pendingSize, maxPendingEdgesSize, executorAddEdges)

	payloadMap := make(map[int]*subMutation)
	var esize int64
	for _, edge := range edges {
		cid := e.channelID(edge)
		payload, ok := payloadMap[cid]
		if !ok {
			payloadMap[cid] = &subMutation{
				ctx:     ctx,
				startTs: startTs,
			}
			payload = payloadMap[cid]
		}
		payload.edges = append(payload.edges, edge)
		esize += int64(edge.Size())
	}

	// RLock() in case the channel gets closed from underneath us.
	e.RLock()
	defer e.RUnlock()
	select {
	case <-e.closer.HasBeenClosed():
		return
	default:
		// Closer is not closed. And we have the RLock, so sending on channel should be safe.
		for cid, payload := range payloadMap {
			e.workerChan[cid] <- payload
		}
	}

	atomic.AddInt64(&e.pendingSize, esize)
}