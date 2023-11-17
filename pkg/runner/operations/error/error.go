package error

import (
	"context"
	"fmt"

	"github.com/kyverno/chainsaw/pkg/client"
	"github.com/kyverno/chainsaw/pkg/runner/logging"
	"github.com/kyverno/chainsaw/pkg/runner/operations/internal"
	"github.com/kyverno/kyverno-json/pkg/engine/assert"
	"github.com/kyverno/kyverno/ext/output/color"
	"go.uber.org/multierr"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
)

type operation struct {
	client   client.Client
	expected unstructured.Unstructured
}

func New(client client.Client, expected unstructured.Unstructured) *operation {
	return &operation{
		client:   client,
		expected: expected,
	}
}

func (o *operation) Exec(ctx context.Context) (_err error) {
	logger := logging.FromContext(ctx).WithResource(&o.expected)
	logger.Log(logging.Error, color.BoldFgCyan, "RUNNING...")
	defer func() {
		if _err == nil {
			logger.Log(logging.Error, color.BoldGreen, "DONE")
		} else {
			logger.Log(logging.Error, color.BoldRed, fmt.Sprintf("ERROR\n%s", _err))
		}
	}()
	var lastErrs []error
	err := wait.PollUntilContextCancel(ctx, internal.PollInterval, false, func(ctx context.Context) (_ bool, err error) {
		var errs []error
		defer func() {
			// record last errors only if there was no real error
			if err == nil {
				lastErrs = errs
			}
		}()
		if candidates, err := internal.Read(ctx, &o.expected, o.client); err != nil {
			if kerrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		} else if len(candidates) == 0 {
			return true, nil
		} else {
			for i := range candidates {
				candidate := candidates[i]
				_errs, err := assert.Validate(ctx, o.expected.UnstructuredContent(), candidate.UnstructuredContent(), nil)
				if err != nil {
					return false, err
				}
				if len(_errs) == 0 {
					errs = append(errs, fmt.Errorf("found an actual resource matching expectation (%s/%s / %s)", candidate.GetAPIVersion(), candidate.GetKind(), client.ObjectKey(&candidate)))
				}
			}
			return len(errs) == 0, nil
		}
	})
	// if no error, return success
	if err == nil {
		return nil
	}
	// eventually return a combination of last errors
	if len(lastErrs) != 0 {
		return multierr.Combine(lastErrs...)
	}
	// return received error
	return err
}