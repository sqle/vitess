// Copyright 2016, Google Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package grpcthrottlerserver contains the gRPC implementation of the server
// side of the throttler service.
package grpcthrottlerserver

import (
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/gitql/vitess/go/vt/servenv"
	"github.com/gitql/vitess/go/vt/throttler"

	"github.com/gitql/vitess/go/vt/proto/throttlerdata"
	"github.com/gitql/vitess/go/vt/proto/throttlerservice"
)

// Server is the gRPC server implementation of the Throttler service.
type Server struct {
	manager throttler.Manager
}

// NewServer creates a new RPC server for a given throttler manager.
func NewServer(m throttler.Manager) *Server {
	return &Server{m}
}

// MaxRates implements the gRPC server interface. It returns the current max
// rate for each throttler of the process.
func (s *Server) MaxRates(_ context.Context, request *throttlerdata.MaxRatesRequest) (_ *throttlerdata.MaxRatesResponse, err error) {
	defer servenv.HandlePanic("throttler", &err)

	rates := s.manager.MaxRates()
	return &throttlerdata.MaxRatesResponse{
		Rates: rates,
	}, nil
}

// SetMaxRate implements the gRPC server interface. It sets the rate on all
// throttlers controlled by the manager.
func (s *Server) SetMaxRate(_ context.Context, request *throttlerdata.SetMaxRateRequest) (_ *throttlerdata.SetMaxRateResponse, err error) {
	defer servenv.HandlePanic("throttler", &err)

	names := s.manager.SetMaxRate(request.Rate)
	return &throttlerdata.SetMaxRateResponse{
		Names: names,
	}, nil
}

// GetConfiguration implements the gRPC server interface.
func (s *Server) GetConfiguration(_ context.Context, request *throttlerdata.GetConfigurationRequest) (_ *throttlerdata.GetConfigurationResponse, err error) {
	defer servenv.HandlePanic("throttler", &err)

	configurations, err := s.manager.GetConfiguration(request.ThrottlerName)
	if err != nil {
		return nil, err
	}
	return &throttlerdata.GetConfigurationResponse{
		Configurations: configurations,
	}, nil
}

// UpdateConfiguration implements the gRPC server interface.
func (s *Server) UpdateConfiguration(_ context.Context, request *throttlerdata.UpdateConfigurationRequest) (_ *throttlerdata.UpdateConfigurationResponse, err error) {
	defer servenv.HandlePanic("throttler", &err)

	names, err := s.manager.UpdateConfiguration(request.ThrottlerName, request.Configuration, request.CopyZeroValues)
	if err != nil {
		return nil, err
	}
	return &throttlerdata.UpdateConfigurationResponse{
		Names: names,
	}, nil
}

// ResetConfiguration implements the gRPC server interface.
func (s *Server) ResetConfiguration(_ context.Context, request *throttlerdata.ResetConfigurationRequest) (_ *throttlerdata.ResetConfigurationResponse, err error) {
	defer servenv.HandlePanic("throttler", &err)

	names, err := s.manager.ResetConfiguration(request.ThrottlerName)
	if err != nil {
		return nil, err
	}
	return &throttlerdata.ResetConfigurationResponse{
		Names: names,
	}, nil
}

// StartServer registers the Server instance with the gRPC server.
func StartServer(s *grpc.Server, m throttler.Manager) {
	throttlerservice.RegisterThrottlerServer(s, NewServer(m))
}

func init() {
	servenv.OnRun(func() {
		if servenv.GRPCCheckServiceMap("throttler") {
			StartServer(servenv.GRPCServer, throttler.GlobalManager)
		}
	})
}
