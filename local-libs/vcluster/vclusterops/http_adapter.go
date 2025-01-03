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
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"time"

	"github.com/vertica/vcluster/rfc7807"
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type httpAdapter struct {
	opBase
	host            string
	respBodyHandler responseBodyHandler
}

func makeHTTPAdapter(logger vlog.Printer) httpAdapter {
	newHTTPAdapter := httpAdapter{}
	newHTTPAdapter.name = "HTTPAdapter"
	newHTTPAdapter.logger = logger.WithName(newHTTPAdapter.name)
	newHTTPAdapter.respBodyHandler = &responseBodyReader{}
	return newHTTPAdapter
}

// makeHTTPDownloadAdapter creates an HTTP adapter which will
// download a response body to a file via streaming read and
// buffered write, rather than copying the body to memory.
func makeHTTPDownloadAdapter(logger vlog.Printer,
	destFilePath string) httpAdapter {
	newHTTPAdapter := makeHTTPAdapter(logger)
	newHTTPAdapter.respBodyHandler = &responseBodyDownloader{
		logger,
		destFilePath,
	}
	return newHTTPAdapter
}

type responseBodyHandler interface {
	processResponseBody(resp *http.Response) (string, error)
}

// empty struct for default behavior of reading response body into memory
type responseBodyReader struct{}

// for downloading response body to file instead of reading into memory
type responseBodyDownloader struct {
	logger       vlog.Printer
	destFilePath string
}

const (
	CertPathBase          = "/opt/vertica/config/https_certs"
	nmaPort               = 5554
	httpsPort             = 8443
	defaultRequestTimeout = 300 // seconds
)

type certificatePaths struct {
	certFile string
	keyFile  string
	caFile   string
}

func (adapter *httpAdapter) sendRequest(request *hostHTTPRequest, resultChannel chan<- hostHTTPResult) {
	// build query params
	queryParams := buildQueryParamString(request.QueryParams)

	// set up the request URL
	var port int
	if request.IsNMACommand {
		port = nmaPort
	} else {
		port = httpsPort
	}

	requestURL := fmt.Sprintf("https://%s:%d/%s%s",
		adapter.host,
		port,
		request.Endpoint,
		queryParams)
	adapter.logger.Info("Request URL", "URL", requestURL)

	// whether use password (for HTTPS endpoints only)
	usePassword, err := whetherUsePassword(request)
	if err != nil {
		resultChannel <- adapter.makeExceptionResult(err)
		return
	}

	// HTTP client
	client, err := adapter.setupHTTPClient(request, usePassword, resultChannel)
	if err != nil {
		resultChannel <- adapter.makeExceptionResult(err)
		return
	}

	// set up request body
	var requestBody io.Reader
	if request.RequestData == "" {
		requestBody = http.NoBody
	} else {
		requestBody = bytes.NewBuffer([]byte(request.RequestData))
	}

	// build HTTP request
	req, err := http.NewRequest(request.Method, requestURL, requestBody)
	if err != nil {
		err = fmt.Errorf("fail to build request %v on host %s, details %w",
			request.Endpoint, adapter.host, err)
		resultChannel <- adapter.makeExceptionResult(err)
		return
	}
	// close the connection after sending the request (for clients)
	req.Close = true

	// set username and password
	// which is only used for HTTPS endpoints
	if usePassword {
		req.SetBasicAuth(request.Username, *request.Password)
	}

	// send HTTP request
	resp, err := client.Do(req)
	if err != nil {
		err = fmt.Errorf("fail to send request %v on host %s, details %w",
			request.Endpoint, adapter.host, err)
		if errors.Is(err, io.EOF) {
			resultChannel <- adapter.makeEOFResult(err)
		} else {
			resultChannel <- adapter.makeExceptionResult(err)
		}
		return
	}
	defer resp.Body.Close()

	// generate and return the result
	resultChannel <- adapter.generateResult(resp)
}

func (adapter *httpAdapter) generateResult(resp *http.Response) hostHTTPResult {
	bodyString, err := adapter.respBodyHandler.processResponseBody(resp)
	if err != nil {
		return adapter.makeExceptionResult(err)
	}
	if isSuccess(resp) {
		return adapter.makeSuccessResult(bodyString, resp.StatusCode)
	}
	return adapter.makeFailResult(resp.Header, bodyString, resp.StatusCode)
}

func (*responseBodyReader) processResponseBody(resp *http.Response) (bodyString string, err error) {
	return readResponseBody(resp)
}

func (downloader *responseBodyDownloader) processResponseBody(resp *http.Response) (bodyString string, err error) {
	if !isSuccess(resp) {
		// in case of error, we get an RFC7807 error, not a file
		return readResponseBody(resp)
	}

	bytesWritten, err := downloader.downloadFile(resp)
	if err != nil {
		err = fmt.Errorf("fail to stream the response body to file %s: %w", downloader.destFilePath, err)
	} else {
		downloader.logger.Info("File downloaded", "File", downloader.destFilePath, "Bytes", bytesWritten)
	}
	return "", err
}

// downloadFile uses buffered read/writes to download the http response body to a file
func (downloader *responseBodyDownloader) downloadFile(resp *http.Response) (bytesWritten int64, err error) {
	file, err := os.Create(downloader.destFilePath)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	return io.Copy(file, resp.Body)
}

// readResponseBody attempts to read the entire contents of the http response into bodyString
func readResponseBody(resp *http.Response) (bodyString string, err error) {
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("fail to read the response body: %w", err)
	}
	bodyString = string(bodyBytes)

	return bodyString, nil
}

func isSuccess(resp *http.Response) bool {
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// makeSuccessResult is a factory method for hostHTTPResult when a success
// response comes back from a REST endpoints.
func (adapter *httpAdapter) makeSuccessResult(content string, statusCode int) hostHTTPResult {
	return hostHTTPResult{
		host:       adapter.host,
		status:     SUCCESS,
		statusCode: statusCode,
		content:    content,
	}
}

// makeExceptionResult is a factory method for hostHTTPResult when an error
// during the process of communicating with a REST endpoint. It won't refer to
// the error received over the wire, but usually some error that occurred in the
// process of communicating.
func (adapter *httpAdapter) makeExceptionResult(err error) hostHTTPResult {
	return hostHTTPResult{
		host:   adapter.host,
		status: EXCEPTION,
		err:    err,
	}
}

// makeFailResult is a factory method for hostHTTPResult when an error response
// is received from a REST endpoint.
func (adapter *httpAdapter) makeFailResult(header http.Header, respBody string, statusCode int) hostHTTPResult {
	return hostHTTPResult{
		host:       adapter.host,
		status:     FAILURE,
		statusCode: statusCode,
		content:    respBody,
		err:        adapter.extractErrorFromResponse(header, respBody, statusCode),
	}
}

// makeEOFResult is a factory method for hostHTTPSResult when an EOF response
// is received from a REST endpoint.
func (adapter *httpAdapter) makeEOFResult(err error) hostHTTPResult {
	return hostHTTPResult{
		host:   adapter.host,
		status: EOFEXCEPTION,
		err:    err,
	}
}

// extractErrorFromResponse is called when we get a failed response from a REST
// call. We will look at the headers and response body to decide what error
// object to create.
func (adapter *httpAdapter) extractErrorFromResponse(header http.Header, respBody string, statusCode int) error {
	if header.Get("Content-Type") == rfc7807.ContentType {
		return rfc7807.GenerateErrorFromResponse(respBody)
	}
	return fmt.Errorf("status code %d returned from host %s: %s", statusCode, adapter.host, respBody)
}

func whetherUsePassword(request *hostHTTPRequest) (bool, error) {
	if request.IsNMACommand {
		return false, nil
	}

	// in case that password is provided
	if request.Password != nil {
		return true, nil
	}

	// otherwise, use certs
	// a. use certs in options
	if request.UseCertsInOptions {
		return false, nil
	}

	// b. use certs in local path
	_, err := getCertFilePaths()
	if err != nil {
		// in case that the cert files do not exist
		return false, fmt.Errorf("either TLS certificates or password should be provided")
	}

	return false, nil
}

// this variable is for unit test, be careful to modify it
var getCertFilePathsFn = getCertFilePaths

func (adapter *httpAdapter) buildCertsFromFile() (tls.Certificate, *x509.CertPool, error) {
	certPaths, err := getCertFilePathsFn()
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("fail to get paths for certificates, details %w", err)
	}

	cert, err := tls.LoadX509KeyPair(certPaths.certFile, certPaths.keyFile)
	if err != nil {
		return cert, nil, fmt.Errorf("fail to load HTTPS certificates, details %w", err)
	}

	caCert, err := os.ReadFile(certPaths.caFile)
	if err != nil {
		return cert, nil, fmt.Errorf("fail to load HTTPS CA certificates, details %w", err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	return cert, caCertPool, nil
}

func (adapter *httpAdapter) buildCertsFromMemory(key, cert, caCert string) (tls.Certificate, *x509.CertPool, error) {
	certificate, err := tls.X509KeyPair([]byte(cert), []byte(key))
	if err != nil {
		return certificate, nil, fmt.Errorf("fail to load HTTPS certificates, details %w", err)
	}

	caCertPool := x509.NewCertPool()
	if caCert != "" {
		ok := caCertPool.AppendCertsFromPEM([]byte(caCert))
		if !ok {
			return certificate, nil, fmt.Errorf("fail to load HTTPS CA certificates")
		}
	}

	return certificate, caCertPool, nil
}

func (adapter *httpAdapter) setupHTTPClient(
	request *hostHTTPRequest,
	usePassword bool,
	_ chan<- hostHTTPResult) (*http.Client, error) {
	// set up request timeout
	requestTimeout := time.Duration(defaultRequestTimeout)
	if request.Timeout > 0 {
		requestTimeout = time.Duration(request.Timeout)
	} else if request.Timeout == -1 {
		requestTimeout = time.Duration(0) // a Timeout of zero means no timeout.
	}

	client := &http.Client{Timeout: time.Second * requestTimeout}
	var config *tls.Config

	if usePassword {
		// TODO: we have to use `InsecureSkipVerify: true` here,
		//       as password is used
		//nolint:gosec
		config = &tls.Config{
			InsecureSkipVerify: true,
		}
	} else {
		var cert tls.Certificate
		var caCertPool *x509.CertPool
		var err error
		if request.UseCertsInOptions {
			cert, caCertPool, err = adapter.buildCertsFromMemory(request.Certs.key, request.Certs.cert, request.Certs.caCert)
		} else {
			cert, caCertPool, err = adapter.buildCertsFromFile()
		}
		if err != nil {
			return client, err
		}

		// by default, skip peer certificate validation, but allow overrides
		//nolint:gosec
		config = &tls.Config{
			Certificates:       []tls.Certificate{cert},
			RootCAs:            caCertPool,
			InsecureSkipVerify: true,
		}
		if request.TLSDoVerify {
			if request.TLSDoVerifyHostname {
				// use the built-in golang verification process to validate certificate signer chain
				// and hostname
				config.InsecureSkipVerify = false
			} else {
				// Note that hosts at this point are IP addresses, so verify-full may be impractical
				// or impossible due to the complications of issuing certificates valid for IPs.
				// Hence the custom validator skipping hostname validation.
				config.VerifyPeerCertificate = util.GenerateTLSVerifyFunc(caCertPool)
			}
		}
	}

	client.Transport = &http.Transport{TLSClientConfig: config}
	return client, nil
}

func buildQueryParamString(queryParams map[string]string) string {
	var queryParamString string
	if len(queryParams) == 0 {
		return queryParamString
	}

	v := url.Values{}
	for key, value := range queryParams {
		v.Set(key, value)
	}
	queryParamString = "?" + v.Encode()
	return queryParamString
}

func getCertFilePaths() (certPaths certificatePaths, err error) {
	username, err := util.GetCurrentUsername()
	if err != nil {
		return certPaths, err
	}

	fixWay := "DBAdmin user can use the --generate-https-certs-only option of install_vertica to regenerate the default certificate bundle"
	certPaths.certFile = path.Join(CertPathBase, username+".pem")
	if !util.CheckPathExist(certPaths.certFile) {
		return certPaths, fmt.Errorf("cert file %q does not exist. "+
			"Please verify that your cert file is in the correct location. %s", certPaths.certFile, fixWay)
	}

	certPaths.keyFile = path.Join(CertPathBase, username+".key")
	if !util.CheckPathExist(certPaths.keyFile) {
		return certPaths, fmt.Errorf("key file %q does not exist. "+
			"Please verify that your key file is in the correct location. %s", certPaths.keyFile, fixWay)
	}

	certPaths.caFile = path.Join(CertPathBase, "rootca.pem")
	if !util.CheckPathExist(certPaths.caFile) {
		return certPaths, fmt.Errorf("ca file %q does not exist. "+
			"Please verify that your ca file is in the correct location. %s", certPaths.caFile, fixWay)
	}

	return certPaths, nil
}
