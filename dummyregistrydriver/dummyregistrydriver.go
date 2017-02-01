package dummyregistrydriver

import (
	"context"
	"github.com/clawio/lib"
)

type driver struct {}

func New() lib.RegistryDriver {
	return &driver{}
}

func (d *driver) Register(ctx context.Context, node lib.RegistryNode) error {
	return nil
}

func (d *driver) GetNodesForRol(ctx context.Context, rol string) ([]lib.RegistryNode, error) {
	return []lib.RegistryNode{}, nil
}
