/*
 (c) Copyright [2023-2024] Open Text.
 Licensed under the Apache License, Version 2.0 (the "License");
 You may not use this file except in compliance with the License.
 You may obtain a copy of the License at

 http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package vclusterops

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestClusteLeaseExpiryError(t *testing.T) {
	op := nmaDownloadFileOp{
		ignoreClusterLease: false,
	}

	// Failure case when lease hasn't expired
	fakeLeaseTime := time.Now().UTC().Add(time.Minute * time.Duration(5))
	err := op.clusterLeaseCheck(fakeLeaseTime.Format(expirationStringLayout))
	assert.Error(t, err)
	// Ensure we get a specific error type back
	clusterLeaseErr := &ClusterLeaseNotExpiredError{}
	ok := errors.As(err, &clusterLeaseErr)
	assert.True(t, ok)
	assert.Contains(t, err.Error(), "The cluster lease will expire at")

	// Success case
	fakeLeaseTime = time.Now().UTC().Add(-time.Minute * time.Duration(5))
	err = op.clusterLeaseCheck(fakeLeaseTime.Format(expirationStringLayout))
	assert.NoError(t, err)
}
