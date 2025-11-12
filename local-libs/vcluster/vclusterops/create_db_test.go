/*
 (c) Copyright [2023-2025] Open Text.
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
	"testing"

	"github.com/stretchr/testify/assert"
)

const defaultPath = "/data"

func TestValidateDepotSize(t *testing.T) {
	res, err := validateDepotSize("-19%")
	assert.Equal(t, res, false)
	assert.NotNil(t, err)
	assert.ErrorContains(t, err, "it is less than 0%")

	res, err = validateDepotSize("119%")
	assert.Equal(t, res, false)
	assert.NotNil(t, err)
	assert.ErrorContains(t, err, "it is greater than 100%")

	res, err = validateDepotSize("+19%")
	assert.Equal(t, res, true)
	assert.Nil(t, err)

	res, err = validateDepotSize("19%")
	assert.Equal(t, res, true)
	assert.Nil(t, err)

	res, err = validateDepotSize("-119K")
	assert.Equal(t, res, false)
	assert.NotNil(t, err)
	assert.ErrorContains(t, err, "it is <= 0")

	res, err = validateDepotSize("+119T")
	assert.Equal(t, res, true)
	assert.Nil(t, err)
}

func TestSpreadMode(t *testing.T) {
	var options VCreateDatabaseOptions
	const communalStorageLocation = "/communal"

	// default spread mode setting: p2p mode for enterprise database
	options.setDefaultValues()
	err := options.validateExtraOptions()
	assert.Nil(t, err)

	// broadcast mode for enterprise database
	options.Broadcast = true
	err = options.validateExtraOptions()
	assert.Nil(t, err)

	// p2p mode for Eon databse
	options.CommunalStorageLocation = communalStorageLocation
	options.Broadcast = false
	err = options.validateExtraOptions()
	assert.Nil(t, err)

	// broadcast mode for Eon database, which should fail
	options.CommunalStorageLocation = communalStorageLocation
	options.Broadcast = true
	err = options.validateExtraOptions()
	assert.NotNil(t, err)
	assert.ErrorContains(t, err, "cannot use broadcast mode in an Eon database")
}
