package main

import (
	"github.com/gitql/vitess/go/vt/discovery"
	"github.com/gitql/vitess/go/vt/servenv"
	_ "github.com/gitql/vitess/go/vt/status"
	"github.com/gitql/vitess/go/vt/vtgate"
	"github.com/gitql/vitess/go/vt/vtgate/gateway"
	"github.com/gitql/vitess/go/vt/vtgate/l2vtgate"
)

// For use by plugins which wish to avoid racing when registering status page parts.
var onStatusRegistered func()

func addStatusParts(l2vtgate *l2vtgate.L2VTGate) {
	servenv.AddStatusPart("Topology Cache", vtgate.TopoTemplate, func() interface{} {
		return resilientSrvTopoServer.CacheStatus()
	})
	servenv.AddStatusPart("Gateway Status", gateway.StatusTemplate, func() interface{} {
		return l2vtgate.GetGatewayCacheStatus()
	})
	servenv.AddStatusPart("Health Check Cache", discovery.HealthCheckTemplate, func() interface{} {
		return healthCheck.CacheStatus()
	})
	if onStatusRegistered != nil {
		onStatusRegistered()
	}
}
