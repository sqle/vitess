// Copyright 2014, Google Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zkcustomrule

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/samuel/go-zookeeper/zk"

	"github.com/gitql/vitess/go/testfiles"
	"github.com/gitql/vitess/go/vt/tabletserver"
	"github.com/gitql/vitess/go/vt/tabletserver/tabletservermock"
	"github.com/gitql/vitess/go/vt/topo/zk2topo"
	"github.com/gitql/vitess/go/zk/zkctl"
)

var customRule1 = `
[
  {
    "Name": "r1",
    "Description": "disallow bindvar 'asdfg'",
    "BindVarConds":[{
      "Name": "asdfg",
      "OnAbsent": false,
      "Operator": ""
    }]
  }
]`

var customRule2 = `
[
  {
    "Name": "r2",
    "Description": "disallow insert on table test",
    "TableNames" : ["test"],
    "Query" : "(insert)|(INSERT)"
  }
]`

func TestZkCustomRule(t *testing.T) {
	// Start a real single ZK daemon, and close it after all tests are done.
	zkd, serverAddr := zkctl.StartLocalZk(testfiles.GoVtTabletserverCustomruleZkcustomruleZkID, testfiles.GoVtTabletserverCustomruleZkcustomrulePort)
	defer zkd.Teardown()

	// Create fake file.
	serverPath := "/zk/fake/customrules/testrules"
	ctx := context.Background()
	conn := zk2topo.Connect(serverAddr)
	defer conn.Close()
	if _, err := zk2topo.CreateRecursive(ctx, conn, serverPath, []byte(customRule1), 0, zk.WorldACL(zk2topo.PermFile), 3); err != nil {
		t.Fatalf("CreateRecursive failed: %v", err)
	}

	// Start a mock tabletserver.
	tqsc := tabletservermock.NewController()

	// Setup the ZkCustomRule
	zkcr := NewZkCustomRule(serverAddr, serverPath)
	err := zkcr.Start(tqsc)
	if err != nil {
		t.Fatalf("Cannot start zookeeper custom rule service: %v", err)
	}
	defer zkcr.Stop()

	var qrs *tabletserver.QueryRules
	// Test if we can successfully fetch the original rule (test GetRules)
	qrs, _, err = zkcr.GetRules()
	if err != nil {
		t.Fatalf("GetRules of ZkCustomRule should always return nil error, but we receive %v", err)
	}
	qr := qrs.Find("r1")
	if qr == nil {
		t.Fatalf("Expect custom rule r1 to be found, but got nothing, qrs=%v", qrs)
	}

	// Test updating rules
	conn.Set(ctx, serverPath, []byte(customRule2), -1)
	<-time.After(time.Second) //Wait for the polling thread to respond
	qrs, _, err = zkcr.GetRules()
	if err != nil {
		t.Fatalf("GetRules of ZkCustomRule should always return nil error, but we receive %v", err)
	}
	qr = qrs.Find("r2")
	if qr == nil {
		t.Fatalf("Expect custom rule r2 to be found, but got nothing, qrs=%v", qrs)
	}
	qr = qrs.Find("r1")
	if qr != nil {
		t.Fatalf("Custom rule r1 should not be found after r2 is set")
	}

	// Test rule path removal
	conn.Delete(ctx, serverPath, -1)
	<-time.After(time.Second)
	qrs, _, err = zkcr.GetRules()
	if err != nil {
		t.Fatalf("GetRules of ZkCustomRule should always return nil error, but we receive %v", err)
	}
	if reflect.DeepEqual(qrs, tabletserver.NewQueryRules()) {
		t.Fatalf("Expect empty rule at this point")
	}

	// Test rule path revival
	conn.Create(ctx, serverPath, []byte("customrule2"), 0, zk.WorldACL(zk2topo.PermFile))
	<-time.After(time.Second) //Wait for the polling thread to respond
	qrs, _, err = zkcr.GetRules()
	if err != nil {
		t.Fatalf("GetRules of ZkCustomRule should always return nil error, but we receive %v", err)
	}
	qr = qrs.Find("r2")
	if qr == nil {
		t.Fatalf("Expect custom rule r2 to be found, but got nothing, qrs=%v", qrs)
	}
}
