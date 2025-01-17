// Copyright 2016, Google Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gateway

import (
	"flag"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/context"

	"github.com/gitql/vitess/go/flagutil"
	"github.com/gitql/vitess/go/vt/discovery"
	"github.com/gitql/vitess/go/vt/key"
	"github.com/gitql/vitess/go/vt/tabletserver/queryservice"
	"github.com/gitql/vitess/go/vt/tabletserver/tabletconn"
	"github.com/gitql/vitess/go/vt/topo"
	"github.com/gitql/vitess/go/vt/topo/topoproto"

	querypb "github.com/gitql/vitess/go/vt/proto/query"
	topodatapb "github.com/gitql/vitess/go/vt/proto/topodata"
	vtrpcpb "github.com/gitql/vitess/go/vt/proto/vtrpc"
)

const (
	gatewayImplementationL2VTGate = "l2vtgategateway"
)

var (
	l2VTGateGatewayAddrs flagutil.StringListValue
)

func init() {
	flag.Var(&l2VTGateGatewayAddrs, "l2vtgategateway_addrs", "Specifies a comma-separated list of 'addr|keyspace|shard_name or keyrange' values for l2vtgate locations")
	RegisterCreator(gatewayImplementationL2VTGate, createL2VTGateGateway)
}

// l2VTGateConn is a connection to a backend l2vtgate pool
type l2VTGateConn struct {
	// set at construction time
	addr     string
	keyspace string
	shard    string
	keyRange *topodatapb.KeyRange // only set if shard is also a KeyRange
	conn     queryservice.QueryService
}

// l2VTGateGateway is the main gateway object
type l2VTGateGateway struct {
	queryservice.QueryService
	// retryCount is set at construction time
	retryCount int

	// mu protects all fields below.
	mu sync.RWMutex
	// connMap is the main map to find the right l2 vtgate pool.
	// It is indexed by keyspace name.
	connMap map[string][]*l2VTGateConn
	// tabletConnMap is a map of address to queryservice.QueryService objects.
	// It is used so we don't open multiple connections to the same backend.
	tabletConnMap map[string]queryservice.QueryService
	// statusAggregators is a map indexed by the key
	// l2vtgate address + tablet type
	statusAggregators map[string]*TabletStatusAggregator
}

func createL2VTGateGateway(hc discovery.HealthCheck, topoServer topo.Server, serv topo.SrvTopoServer, cell string, retryCount int) Gateway {
	lg := &l2VTGateGateway{
		retryCount:        retryCount,
		connMap:           make(map[string][]*l2VTGateConn),
		tabletConnMap:     make(map[string]queryservice.QueryService),
		statusAggregators: make(map[string]*TabletStatusAggregator),
	}

	for _, a := range l2VTGateGatewayAddrs {
		parts := strings.Split(a, "|")
		if len(parts) != 3 {
			log.Fatalf("invalid l2vtgategateway_addrs parameter: %v", a)
		}

		if err := lg.addL2VTGateConn(parts[0], parts[1], parts[2]); err != nil {
			log.Fatalf("error adding l2vtgategateway_addrs value %v: %v", a, err)
		}
	}
	lg.QueryService = queryservice.Wrap(nil, lg.withRetry)

	return lg
}

// addL2VTGateConn adds a backend l2vtgate for the provided keyspace / shard.
func (lg *l2VTGateGateway) addL2VTGateConn(addr, keyspace, shard string) error {
	lg.mu.Lock()
	defer lg.mu.Unlock()

	// extract keyrange if it's a range
	canonical, kr, err := topo.ValidateShardName(shard)
	if err != nil {
		return fmt.Errorf("error parsing shard name %v: %v", shard, err)
	}

	// check for duplicates
	for _, c := range lg.connMap[keyspace] {
		if c.shard == canonical {
			return fmt.Errorf("duplicate %v/%v entry", keyspace, shard)
		}
	}

	// See if we already have a valid connection
	conn, ok := lg.tabletConnMap[addr]
	if !ok {
		// Dial in the background, as specified by timeout=0.
		conn, err = tabletconn.GetDialer()(&topodatapb.Tablet{
			Hostname: addr,
		}, 0)
		if err != nil {
			return err
		}
		lg.tabletConnMap[addr] = conn
	}

	lg.connMap[keyspace] = append(lg.connMap[keyspace], &l2VTGateConn{
		addr:     addr,
		keyspace: keyspace,
		shard:    canonical,
		keyRange: kr,
		conn:     conn,
	})
	return nil
}

// WaitForTablets is part of the Gateway interface. We don't implement it,
// as we don't have anything to wait for.
func (lg *l2VTGateGateway) WaitForTablets(ctx context.Context, tabletTypesToWait []topodatapb.TabletType) error {
	return nil
}

// StreamHealth is currently not implemented.
// This function hides the inner implementation.
// TODO(alainjobart): Maybe we should?
func (lg *l2VTGateGateway) StreamHealth(ctx context.Context, callback func(*querypb.StreamHealthResponse) error) error {
	panic("not implemented")
}

// Close shuts down underlying connections.
// This function hides the inner implementation.
func (lg *l2VTGateGateway) Close(ctx context.Context) error {
	lg.mu.Lock()
	defer lg.mu.Unlock()

	// This will wait for all on-going queries before returning.
	for _, c := range lg.tabletConnMap {
		c.Close(ctx)
	}
	lg.tabletConnMap = make(map[string]queryservice.QueryService)
	lg.connMap = make(map[string][]*l2VTGateConn)
	return nil
}

// CacheStatus returns a list of TabletCacheStatus per
// keyspace/shard/tablet_type.
func (lg *l2VTGateGateway) CacheStatus() TabletCacheStatusList {
	lg.mu.RLock()
	res := make(TabletCacheStatusList, 0, len(lg.statusAggregators))
	for _, aggr := range lg.statusAggregators {
		res = append(res, aggr.GetCacheStatus())
	}
	lg.mu.RUnlock()
	sort.Sort(res)
	return res
}

// getConn returns the right l2VTGateConn for a given keyspace / shard.
func (lg *l2VTGateGateway) getConn(keyspace, shard string) (*l2VTGateConn, error) {
	lg.mu.RLock()
	defer lg.mu.RUnlock()

	canonical, kr, err := topo.ValidateShardName(shard)
	if err != nil {
		return nil, fmt.Errorf("invalid shard name: %v", shard)
	}

	for _, c := range lg.connMap[keyspace] {
		if canonical == c.shard {
			// Exact match (probably a non-sharded keyspace).
			return c, nil
		}
		if kr != nil && c.keyRange != nil && key.KeyRangeIncludes(c.keyRange, kr) {
			// The shard KeyRange is included in this destination's
			// KeyRange, that's the destination we want.
			return c, nil
		}
	}

	return nil, fmt.Errorf("no configured destination for %v/%v", keyspace, shard)
}

// withRetry gets available connections and executes the action. If there are retryable errors,
// it retries retryCount times before failing. It does not retry if the connection is in
// the middle of a transaction. While returning the error check if it maybe a result of
// a resharding event, and set the re-resolve bit and let the upper layers
// re-resolve and retry.
func (lg *l2VTGateGateway) withRetry(ctx context.Context, target *querypb.Target, conn queryservice.QueryService, name string, inTransaction, isStreaming bool, inner func(context.Context, *querypb.Target, queryservice.QueryService) error) error {
	l2conn, err := lg.getConn(target.Keyspace, target.Shard)
	if err != nil {
		return fmt.Errorf("no configured destination for %v/%v: %v", target.Keyspace, target.Shard, err)
	}

	for i := 0; i < lg.retryCount+1; i++ {
		startTime := time.Now()
		err = inner(ctx, target, l2conn.conn)
		lg.updateStats(l2conn, target.TabletType, startTime, err)
		if lg.canRetry(ctx, err, inTransaction, isStreaming) {
			continue
		}
		break
	}
	return NewShardError(err, target, nil, inTransaction)
}

// canRetry determines whether a query can be retried or not.
// OperationalErrors like retry/fatal are retryable if query is not in a txn.
// All other errors are non-retryable.
func (lg *l2VTGateGateway) canRetry(ctx context.Context, err error, inTransaction, isStreaming bool) bool {
	if err == nil {
		return false
	}
	// Do not retry if ctx.Done() is closed.
	select {
	case <-ctx.Done():
		return false
	default:
	}
	if serverError, ok := err.(*tabletconn.ServerError); ok {
		switch serverError.ServerCode {
		case vtrpcpb.ErrorCode_INTERNAL_ERROR:
			// Do not retry on fatal error for streaming query.
			// For streaming query, vttablet sends:
			// - QUERY_NOT_SERVED, if streaming is not started yet;
			// - INTERNAL_ERROR, if streaming is broken halfway.
			// For non-streaming query, handle as QUERY_NOT_SERVED.
			if isStreaming {
				return false
			}
			fallthrough
		case vtrpcpb.ErrorCode_QUERY_NOT_SERVED:
			// Retry on QUERY_NOT_SERVED and
			// INTERNAL_ERROR if not in a transaction.
			return !inTransaction
		default:
			// Not retry for RESOURCE_EXHAUSTED and normal
			// server errors.
			return false
		}
	}
	// Do not retry on operational error.
	return false
}

func (lg *l2VTGateGateway) updateStats(conn *l2VTGateConn, tabletType topodatapb.TabletType, startTime time.Time, err error) {
	elapsed := time.Now().Sub(startTime)
	aggr := lg.getStatsAggregator(conn, tabletType)
	aggr.UpdateQueryInfo("", tabletType, elapsed, err != nil)
}

func (lg *l2VTGateGateway) getStatsAggregator(conn *l2VTGateConn, tabletType topodatapb.TabletType) *TabletStatusAggregator {
	key := fmt.Sprintf("%v:%v", conn.addr, topoproto.TabletTypeLString(tabletType))

	// get existing aggregator
	lg.mu.RLock()
	aggr, ok := lg.statusAggregators[key]
	lg.mu.RUnlock()
	if ok {
		return aggr
	}
	// create a new one, but check again before the creation
	lg.mu.Lock()
	defer lg.mu.Unlock()
	aggr, ok = lg.statusAggregators[key]
	if ok {
		return aggr
	}
	aggr = NewTabletStatusAggregator(conn.keyspace, conn.shard, tabletType, key)
	lg.statusAggregators[key] = aggr
	return aggr
}
