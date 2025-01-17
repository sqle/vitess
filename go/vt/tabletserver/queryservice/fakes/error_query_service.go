package fakes

import (
	"fmt"

	"github.com/gitql/vitess/go/vt/tabletserver/queryservice"
	"golang.org/x/net/context"

	querypb "github.com/gitql/vitess/go/vt/proto/query"
)

// ErrorQueryService is an object that returns an error for all methods.
var ErrorQueryService = queryservice.Wrap(
	nil,
	func(ctx context.Context, target *querypb.Target, conn queryservice.QueryService, name string, inTransaction, isStreaming bool, inner func(context.Context, *querypb.Target, queryservice.QueryService) error) error {
		return fmt.Errorf("ErrorQueryService does not implement any method")
	},
)
