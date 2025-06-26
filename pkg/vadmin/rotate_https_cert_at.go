package vadmin

import (
	"context"
	"errors"

	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatetlscerts"
)

func (a *Admintools) RotateTLSCerts(ctx context.Context, opts ...rotatetlscerts.Option) error {
	return errors.New("RotateTLSCerts is not supported for admintools deployments")
}
