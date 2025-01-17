package topoproto

import (
	"bytes"
	"sort"

	topodatapb "github.com/gitql/vitess/go/vt/proto/topodata"
)

// ShardReferenceArray is used for sorting ShardReference arrays
type ShardReferenceArray []*topodatapb.ShardReference

// Len implements sort.Interface
func (sra ShardReferenceArray) Len() int { return len(sra) }

// Len implements sort.Interface
func (sra ShardReferenceArray) Less(i, j int) bool {
	if sra[i].KeyRange == nil || len(sra[i].KeyRange.Start) == 0 {
		return true
	}
	if sra[j].KeyRange == nil || len(sra[j].KeyRange.Start) == 0 {
		return false
	}
	return bytes.Compare(sra[i].KeyRange.Start, sra[j].KeyRange.Start) < 0
}

// Len implements sort.Interface
func (sra ShardReferenceArray) Swap(i, j int) {
	sra[i], sra[j] = sra[j], sra[i]
}

// Sort will sort the list according to KeyRange.Start
func (sra ShardReferenceArray) Sort() { sort.Sort(sra) }

// SrvKeyspaceGetPartition returns a Partition for the given tablet type,
// or nil if it's not there.
func SrvKeyspaceGetPartition(sk *topodatapb.SrvKeyspace, tabletType topodatapb.TabletType) *topodatapb.SrvKeyspace_KeyspacePartition {
	for _, p := range sk.Partitions {
		if p.ServedType == tabletType {
			return p
		}
	}
	return nil
}
