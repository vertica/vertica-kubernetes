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
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vertica/vcluster/rfc7807"
)

func TestBuildQueryParams(t *testing.T) {
	queryParams := make(map[string]string)

	// empty query params should produce an empty string
	queryParamString := buildQueryParamString(queryParams)
	assert.Empty(t, queryParamString)

	// non-empty query params should produce a string like
	// "?key1=value1&key2=value2"
	queryParams["key1"] = "value1"
	queryParams["key2"] = "value2"
	queryParamString = buildQueryParamString(queryParams)
	assert.Equal(t, queryParamString, "?key1=value1&key2=value2")

	// query params with special characters, such as %
	// which is used by the create depot endpoint
	queryParams = make(map[string]string)
	queryParams["size"] = "10%"
	queryParams["path"] = "/the/depot/path"
	queryParamString = buildQueryParamString(queryParams)
	// `/` is escaped with `%2F`
	// `%` is escaped with `%25`
	assert.Equal(t, queryParamString, "?path=%2Fthe%2Fdepot%2Fpath&size=10%25")
}

func getCertFilePathsMock() (certPaths certificatePaths, err error) {
	basePath := "test_data"
	certPaths.certFile = path.Join(basePath, "test.pem")
	certPaths.keyFile = path.Join(basePath, "test.key")
	certPaths.caFile = path.Join(basePath, "rootca.pem")
	return certPaths, nil
}

func TestBuildCertsFromMemory(t *testing.T) {
	adapter := httpAdapter{}

	// get cert and cacert using buildCertsFromFile()
	originalFunc := getCertFilePaths
	// use the mock function in buildCertsFromFile()
	getCertFilePathsFn = getCertFilePathsMock
	cert1, caCertPool1, err := adapter.buildCertsFromFile()
	if err != nil {
		t.Errorf("fail to execute buildCertsFromFile() %v", err)
	}
	getCertFilePathsFn = originalFunc

	// get cert and cacert using buildCertsFromMemory()
	certPaths, err := getCertFilePathsMock()
	if err != nil {
		t.Errorf("fail to get paths for certificates, details %v", err)
	}
	key, err := os.ReadFile(certPaths.keyFile)
	if err != nil {
		t.Errorf("fail to load HTTPS key, details %v", err)
	}
	cert, err := os.ReadFile(certPaths.certFile)
	if err != nil {
		t.Errorf("fail to load HTTPS certificate, details %v", err)
	}
	caCert, err := os.ReadFile(certPaths.caFile)
	if err != nil {
		t.Errorf("fail to load HTTPS CA certificates, details %v", err)
	}
	cert2, caCertPool2, err := adapter.buildCertsFromMemory(string(key), string(cert), string(caCert))
	if err != nil {
		t.Errorf("fail to execute buildCertsFromFile() %v", err)
	}

	// compare tls.Certificate
	if !reflect.DeepEqual(cert1.Certificate, cert2.Certificate) {
		t.Errorf("Certificates are not the same")
	}
	if !reflect.DeepEqual(cert1.PrivateKey, cert2.PrivateKey) {
		t.Errorf("Private keys are not the same")
	}

	// Compare x509.CertPool
	if !caCertPool1.Equal(caCertPool2) {
		t.Errorf("Cert Pools are not the same")
	}
}

type MockReadCloser struct {
	read bool
	body []byte
}

func (m *MockReadCloser) Read(p []byte) (n int, err error) {
	if !m.read {
		m.read = true
		copy(p, m.body)
		return len(m.body), nil
	}
	return 0, io.EOF
}

func (m *MockReadCloser) Close() error {
	return nil
}

func TestHandleSuccessResponseCodes(t *testing.T) {
	adapter := httpAdapter{respBodyHandler: &responseBodyReader{}}
	mockBodyReader := MockReadCloser{
		body: []byte("success!"),
	}
	mockResp := &http.Response{
		StatusCode: 250,
		Body:       &mockBodyReader,
	}
	result := adapter.generateResult(mockResp)
	assert.Equal(t, result.status, SUCCESS)
	assert.Equal(t, result.err, nil)
}

func TestHandleRFC7807Response(t *testing.T) {
	adapter := httpAdapter{respBodyHandler: &responseBodyReader{}}
	detail := "Cannot access communal storage"
	rfcErr := rfc7807.New(rfc7807.CommunalAccessError).
		WithDetail(detail)
	b, err := json.Marshal(rfcErr)
	assert.Equal(t, err, nil)
	mockBodyReader := MockReadCloser{
		body: b,
	}
	mockResp := &http.Response{
		StatusCode: rfcErr.Status,
		Header:     http.Header{},
		Body:       &mockBodyReader,
	}
	mockResp.Header.Add("Content-Type", rfc7807.ContentType)
	result := adapter.generateResult(mockResp)
	assert.Equal(t, result.status, FAILURE)
	assert.NotEqual(t, result.err, nil)
	problem := &rfc7807.VProblem{}
	ok := errors.As(result.err, &problem)
	assert.True(t, ok)
	assert.Equal(t, 500, problem.Status)
	assert.Equal(t, detail, problem.Detail)
}

func TestHandleFileDownloadErrorResponse(t *testing.T) {
	adapter := httpAdapter{respBodyHandler: &responseBodyDownloader{destFilePath: "/never/use/me"}}
	detail := "Something went horribly wrong and this is not a file"
	rfcErr := rfc7807.New(rfc7807.GenericHTTPInternalServerError).
		WithDetail(detail)
	b, err := json.Marshal(rfcErr)
	assert.Equal(t, err, nil)
	mockBodyReader := MockReadCloser{
		body: b,
	}
	mockResp := &http.Response{
		StatusCode: rfcErr.Status,
		Header:     http.Header{},
		Body:       &mockBodyReader,
	}
	mockResp.Header.Add("Content-Type", rfc7807.ContentType)
	result := adapter.generateResult(mockResp)
	assert.Equal(t, result.status, FAILURE)
	assert.NotEqual(t, result.err, nil)
	problem := &rfc7807.VProblem{}
	ok := errors.As(result.err, &problem)
	assert.True(t, ok)
	assert.Equal(t, 500, problem.Status)
	assert.Equal(t, detail, problem.Detail)
}

func TestHandleGenericErrorResponse(t *testing.T) {
	const errorMessage = "generic error!"
	mockBodyReader := MockReadCloser{
		body: []byte(errorMessage),
	}
	mockResp := &http.Response{
		StatusCode: 500,
		Header:     http.Header{},
		Body:       &mockBodyReader,
	}
	adapter := httpAdapter{respBodyHandler: &responseBodyReader{}}
	result := adapter.generateResult(mockResp)
	assert.Equal(t, result.status, FAILURE)
	assert.NotEqual(t, result.err, nil)
	problem := &rfc7807.VProblem{}
	ok := errors.As(result.err, &problem)
	assert.False(t, ok)
	assert.Contains(t, result.err.Error(), errorMessage)
}
