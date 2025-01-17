package etcdtopo

import (
	"fmt"
	"path"

	"golang.org/x/net/context"

	"github.com/coreos/go-etcd/etcd"
	"github.com/gitql/vitess/go/vt/topo"
)

// Create is part of the topo.Backend interface.
func (s *Server) Create(ctx context.Context, cell, filePath string, contents []byte) (topo.Version, error) {
	c, err := s.clientForCell(cell)
	if err != nil {
		return nil, err
	}

	resp, err := c.Create(filePath, string(contents), 0 /* ttl */)
	if err != nil {
		return nil, convertError(err)
	}
	return EtcdVersion(resp.Node.ModifiedIndex), nil
}

// Update is part of the topo.Backend interface.
func (s *Server) Update(ctx context.Context, cell, filePath string, contents []byte, version topo.Version) (topo.Version, error) {
	c, err := s.clientForCell(cell)
	if err != nil {
		return nil, err
	}

	var resp *etcd.Response
	if version == nil {
		resp, err = c.Set(filePath, string(contents), 0 /* ttl */)
	} else {
		resp, err = c.CompareAndSwap(filePath, string(contents), 0 /* ttl */, "" /* prevValue */, uint64(version.(EtcdVersion)))
	}
	if err != nil {
		return nil, convertError(err)
	}
	return EtcdVersion(resp.Node.ModifiedIndex), nil
}

// Get is part of the topo.Backend interface.
func (s *Server) Get(ctx context.Context, cell, filePath string) ([]byte, topo.Version, error) {
	c, err := s.clientForCell(cell)
	if err != nil {
		return nil, nil, err
	}

	resp, err := c.Get(filePath, false /* sort */, false /* recursive */)
	if err != nil {
		return nil, nil, convertError(err)
	}
	if resp.Node == nil {
		return nil, nil, fmt.Errorf("got bad empty node for %v", filePath)
	}
	return []byte(resp.Node.Value), EtcdVersion(resp.Node.ModifiedIndex), nil
}

// Delete is part of the topo.Backend interface.
func (s *Server) Delete(ctx context.Context, cell, filePath string, version topo.Version) error {
	c, err := s.clientForCell(cell)
	if err != nil {
		return err
	}

	if version == nil {
		_, err = c.Delete(filePath, false /* recursive */)
	} else {
		_, err = c.CompareAndDelete(filePath, "" /* prevValue */, uint64(version.(EtcdVersion)))
	}
	if err != nil {
		return convertError(err)
	}

	// Now recursively delete the parent dirs if empty
	return s.recursiveDeleteParentIfEmpty(ctx, c, filePath)
}

func (s *Server) recursiveDeleteParentIfEmpty(ctx context.Context, c Client, filePath string) error {
	dir := path.Dir(filePath)
	if dir == "" || dir == "/" {
		// we reached the top
		return nil
	}
	_, err := c.DeleteDir(dir)
	if err == nil {
		// this worked, keep going up
		return s.recursiveDeleteParentIfEmpty(ctx, c, dir)
	}
	etcdErr, ok := err.(*etcd.EtcdError)
	if !ok {
		return err
	}
	if etcdErr.ErrorCode == EcodeDirNotEmpty || etcdErr.ErrorCode == EcodeKeyNotFound {
		// We found a non-empty dir, or someone else already
		// removed the directory, done.
		return nil
	}
	return etcdErr
}
