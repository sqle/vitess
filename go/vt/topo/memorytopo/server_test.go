package memorytopo

import (
	"testing"

	"github.com/gitql/vitess/go/vt/topo"
	"github.com/gitql/vitess/go/vt/topo/test"
)

func TestMemoryTopo(t *testing.T) {
	// Run the TopoServerTestSuite tests.
	test.TopoServerTestSuite(t, func() topo.Impl {
		return New("test")
	})
}
