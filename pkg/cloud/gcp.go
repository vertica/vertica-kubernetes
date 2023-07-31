/*
 (c) Copyright [2021-2023] Open Text.
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

package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"

	gsm "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
)

// ReadFromGSM will fetch a secret from Google Secret Manager (GSM)
func ReadFromGSM(ctx context.Context, secName string) (map[string]string, error) {
	clnt, err := gsm.NewClient(ctx)
	if err != nil {
		return make(map[string]string), fmt.Errorf("failed to create secretmanager client")
	}
	defer clnt.Close()

	req := &secretmanagerpb.AccessSecretVersionRequest{
		Name: secName,
	}

	result, err := clnt.AccessSecretVersion(ctx, req)
	if err != nil {
		return make(map[string]string), fmt.Errorf("could not fetch secret: %w", err)
	}

	crc32c := crc32.MakeTable(crc32.Castagnoli)
	checksum := int64(crc32.Checksum(result.Payload.Data, crc32c))
	if checksum != *result.Payload.DataCrc32C {
		return make(map[string]string), fmt.Errorf("data corruption detected")
	}
	contents := make(map[string]string)
	err = json.Unmarshal(result.Payload.Data, &contents)
	if err != nil {
		return make(map[string]string), err
	}
	return contents, nil
}
