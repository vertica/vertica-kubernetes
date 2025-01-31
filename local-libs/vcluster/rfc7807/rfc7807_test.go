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

package rfc7807

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVProblemImplementsError(t *testing.T) {
	p := New(CommunalStorageNotEmpty).
		WithDetail("Path /communal needs to be empty").
		WithHost("pod-0")
	var err1 error
	var ExpectedErrorStr = fmt.Sprintf("%s on host pod-0", CommunalStorageNotEmpty.Title)
	err1 = p // Make sure we can assign p to an error type
	err2 := fmt.Errorf("hit error and wrapping %w", p)
	assert.Contains(t, err1.Error(), ExpectedErrorStr)
	assert.Contains(t, err2.Error(), ExpectedErrorStr)
}

func TestWeCanTestProblemType(t *testing.T) {
	p := New(GenericBootstrapCatalogFailure).
		WithDetail("Internal error was hit during bootstrap catalog").
		WithHost("pod-1")
	assert.True(t, p.IsInstanceOf(GenericBootstrapCatalogFailure))
	assert.False(t, p.IsInstanceOf(CommunalRWAccessError))
}

func TestHttpResponse(t *testing.T) {
	p := New(CommunalAccessError).
		WithDetail("communal endpoint is down").
		WithHost("pod-2")
	handler := func(w http.ResponseWriter, _ *http.Request) {
		p.SendError(w)
	}

	req := httptest.NewRequest("GET", "http://vertica.com/bootstrapEndpoint", http.NoBody)
	w := httptest.NewRecorder()
	handler(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, p.Status, resp.StatusCode)
	assert.Equal(t, ContentType, resp.Header.Get("Content-Type"))
	assert.Contains(t, string(body), p.Detail)
}

func TestProblemExtraction(t *testing.T) {
	origProblem := New(CommunalRWAccessError).
		WithDetail("could not read from communal storage").
		WithHost("pod-3")
	handler := func(w http.ResponseWriter, _ *http.Request) {
		origProblem.SendError(w)
	}

	req := httptest.NewRequest("GET", "http://vertica.com/bootstrapEndpoint", http.NoBody)
	w := httptest.NewRecorder()
	handler(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	err := GenerateErrorFromResponse(string(body))
	assert.NotEqual(t, err, nil)
	extractProblem := &VProblem{}
	ok := errors.As(err, &extractProblem)
	assert.True(t, ok)
	assert.Equal(t, origProblem.Detail, extractProblem.Detail)
	assert.Equal(t, origProblem.Host, extractProblem.Host)
	assert.Equal(t, origProblem.Status, extractProblem.Status)
	assert.Equal(t, origProblem.Title, extractProblem.Title)
	assert.Equal(t, origProblem.Type, extractProblem.Type)
	assert.True(t, reflect.DeepEqual(origProblem, extractProblem))
}

func TestJSONExtractFailure(t *testing.T) {
	handler := func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "not json")
	}
	req := httptest.NewRequest("GET", "http://vertica.com/bootstrapEndpoint", http.NoBody)
	w := httptest.NewRecorder()
	handler(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	err := GenerateErrorFromResponse(string(body))
	assert.NotEqual(t, err, nil)
	extractProblem := &VProblem{}
	ok := errors.As(err, &extractProblem)
	assert.False(t, ok)
	assert.Contains(t, err.Error(), "failed to unmarshal the rfc7807 response")
}
