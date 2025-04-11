package vadmin

import (
	"context"
	"errors"

	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatehttpscerts"
)

func (a *Admintools) RotateHTTPSCerts(ctx context.Context, opts ...rotatehttpscerts.Option) error {
	return errors.New("RotateHTTPSCerts is not supported for admintools deployments")
}
