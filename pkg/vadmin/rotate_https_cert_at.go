package vadmin

import (
	"context"
	"errors"

	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatehttpscerts"
)

func (a *Admintools) RotateTLSCerts(ctx context.Context, opts ...rotatehttpscerts.Option) error {
	return errors.New("RotateTLSCerts is not supported for admintools deployments")
}
