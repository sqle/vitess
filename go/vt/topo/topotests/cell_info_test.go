package topotests

import (
	"fmt"
	"testing"

	"golang.org/x/net/context"

	"github.com/gitql/vitess/go/vt/topo"
	"github.com/gitql/vitess/go/vt/topo/memorytopo"

	topodatapb "github.com/gitql/vitess/go/vt/proto/topodata"
)

// This file tests the CellInfo part of the topo.Server API.

func TestCellInfo(t *testing.T) {
	cell := "cell1"
	ctx := context.Background()
	ts := topo.Server{Impl: memorytopo.New(cell)}

	// Check GetCellInfo returns what memorytopo created.
	ci, err := ts.GetCellInfo(ctx, cell)
	if err != nil {
		t.Fatalf("GetCellInfo failed: %v", err)
	}
	if ci.Root != "/" {
		t.Fatalf("unexpected CellInfo: %v", ci)
	}

	// Update the Server Address.
	if err := ts.UpdateCellInfoFields(ctx, cell, func(ci *topodatapb.CellInfo) error {
		ci.ServerAddress = "new address"
		return nil
	}); err != nil {
		t.Fatalf("UpdateCellInfoFields failed: %v", err)
	}
	ci, err = ts.GetCellInfo(ctx, cell)
	if err != nil {
		t.Fatalf("GetCellInfo failed: %v", err)
	}
	if ci.ServerAddress != "new address" {
		t.Fatalf("unexpected CellInfo: %v", ci)
	}

	// Test update with no change.
	if err := ts.UpdateCellInfoFields(ctx, cell, func(ci *topodatapb.CellInfo) error {
		ci.ServerAddress = "bad address"
		return topo.ErrNoUpdateNeeded
	}); err != nil {
		t.Fatalf("UpdateCellInfoFields failed: %v", err)
	}
	ci, err = ts.GetCellInfo(ctx, cell)
	if err != nil {
		t.Fatalf("GetCellInfo failed: %v", err)
	}
	if ci.ServerAddress != "new address" {
		t.Fatalf("unexpected CellInfo: %v", ci)
	}

	// Test failing update.
	updateErr := fmt.Errorf("inside error")
	if err := ts.UpdateCellInfoFields(ctx, cell, func(ci *topodatapb.CellInfo) error {
		return updateErr
	}); err != updateErr {
		t.Fatalf("UpdateCellInfoFields failed: %v", err)
	}

	// Test update on non-existing object.
	newCell := "new_cell"
	if err := ts.UpdateCellInfoFields(ctx, newCell, func(ci *topodatapb.CellInfo) error {
		ci.Root = "/"
		ci.ServerAddress = "good address"
		return nil
	}); err != nil {
		t.Fatalf("UpdateCellInfoFields failed: %v", err)
	}
	ci, err = ts.GetCellInfo(ctx, newCell)
	if err != nil {
		t.Fatalf("GetCellInfo failed: %v", err)
	}
	if ci.ServerAddress != "good address" || ci.Root != "/" {
		t.Fatalf("unexpected CellInfo: %v", ci)
	}

	// Might as well test DeleteCellInfo.
	if err := ts.DeleteCellInfo(ctx, newCell); err != nil {
		t.Fatalf("DeleteCellInfo failed: %v", err)
	}
	if _, err := ts.GetCellInfo(ctx, newCell); err != topo.ErrNoNode {
		t.Fatalf("GetCellInfo(non-existing cell) failed: %v", err)
	}
}
