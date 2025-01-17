// Automatically generated by MockGen. DO NOT EDIT!
// Source: resharding_wrangler.go

package resharding

import (
	time "time"

	gomock "github.com/golang/mock/gomock"
	topodata "github.com/gitql/vitess/go/vt/proto/topodata"
	context "golang.org/x/net/context"
)

// Mock of ReshardingWrangler interface
type MockReshardingWrangler struct {
	ctrl     *gomock.Controller
	recorder *_MockReshardingWranglerRecorder
}

// Recorder for MockReshardingWrangler (not exported)
type _MockReshardingWranglerRecorder struct {
	mock *MockReshardingWrangler
}

func NewMockReshardingWrangler(ctrl *gomock.Controller) *MockReshardingWrangler {
	mock := &MockReshardingWrangler{ctrl: ctrl}
	mock.recorder = &_MockReshardingWranglerRecorder{mock}
	return mock
}

func (_m *MockReshardingWrangler) EXPECT() *_MockReshardingWranglerRecorder {
	return _m.recorder
}

func (_m *MockReshardingWrangler) CopySchemaShardFromShard(ctx context.Context, tables []string, excludeTables []string, includeViews bool, sourceKeyspace string, sourceShard string, destKeyspace string, destShard string, waitSlaveTimeout time.Duration) error {
	ret := _m.ctrl.Call(_m, "CopySchemaShardFromShard", ctx, tables, excludeTables, includeViews, sourceKeyspace, sourceShard, destKeyspace, destShard, waitSlaveTimeout)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockReshardingWranglerRecorder) CopySchemaShardFromShard(arg0, arg1, arg2, arg3, arg4, arg5, arg6, arg7, arg8 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "CopySchemaShardFromShard", arg0, arg1, arg2, arg3, arg4, arg5, arg6, arg7, arg8)
}

func (_m *MockReshardingWrangler) WaitForFilteredReplication(ctx context.Context, keyspace string, shard string, maxDelay time.Duration) error {
	ret := _m.ctrl.Call(_m, "WaitForFilteredReplication", ctx, keyspace, shard, maxDelay)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockReshardingWranglerRecorder) WaitForFilteredReplication(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "WaitForFilteredReplication", arg0, arg1, arg2, arg3)
}

func (_m *MockReshardingWrangler) MigrateServedTypes(ctx context.Context, keyspace string, shard string, cells []string, servedType topodata.TabletType, reverse bool, skipReFreshState bool, filteredReplicationWaitTime time.Duration) error {
	ret := _m.ctrl.Call(_m, "MigrateServedTypes", ctx, keyspace, shard, cells, servedType, reverse, skipReFreshState, filteredReplicationWaitTime)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockReshardingWranglerRecorder) MigrateServedTypes(arg0, arg1, arg2, arg3, arg4, arg5, arg6, arg7 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "MigrateServedTypes", arg0, arg1, arg2, arg3, arg4, arg5, arg6, arg7)
}
