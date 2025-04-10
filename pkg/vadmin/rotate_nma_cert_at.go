package vadmin

import (
	"context"
	"errors"

	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatenmacerts"
)

func (a *Admintools) RotateNMACerts(ctx context.Context, opts ...rotatenmacerts.Option) error {
	return errors.New("RotateNMACerts is not supported for admintools deployments")
}
