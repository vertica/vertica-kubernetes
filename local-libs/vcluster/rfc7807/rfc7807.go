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
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

const (
	ContentType = "application/problem+json"
)

// Problem is the interface to describe an HTTP API problem.
type Problem interface {
	Error() string
	SendError(w http.ResponseWriter)
}

// ProblemID will identify a problem. Treat this as immutable, especially once a
// version of the server is released with this. If a change is required, then a new
// ProblemID should be generated.
type ProblemID struct {
	// Type contains a URI that identifies the problem type. This will be a link
	// into the vertica docs that explains the error in more detail.
	Type string `json:"type"`

	// Title is a short, human-readable summary of the problem type. This should
	// not change from occurrence to occurrence of the problem, except for
	// purposes of localization.
	Title string `json:"title"`

	// Status is the HTTP status code for this occurrence of the problem.
	Status int `json:"status,omitempty"`
}

// VProblem is vertica's implementation of the RFC 7807 standard.
type VProblem struct {
	ProblemID

	// A human-readable explanation specific to this occurrence of the problem.
	// Include any pertinent info in here to help them resolve the problem.
	Detail string `json:"detail,omitempty"`

	// Host is the vertica host name of IP where the problem occurred.
	Host string `json:"host,omitempty"`
}

// Error implement this function so that VProblem can be passed around with Go's
// error interface.
func (v *VProblem) Error() string {
	return fmt.Sprintf("%s on host %s, detail: %s", v.Title, v.Host, v.Detail)
}

// New will return a new VProblem object. Each occurrence must have the
// type and title, which is why those two are parameters here. The other fields
// in the VProblem struct can be added after this call (see the With* helpers in
// the VProblem struct).
func New(id ProblemID) *VProblem {
	return &VProblem{
		ProblemID: id,
	}
}

// MakeFromResponse will generate a VProblem parsed from a response string
// passed in. The VProblem will flow back as an error interface. You cannot
// always assume a VProblem is flowed back -- there could be a problem parsing
// the response. Callers should always use errors.At() function to check if it
// is in fact a VProblem type.
func GenerateErrorFromResponse(resp string) error {
	prob := VProblem{}
	if err := json.Unmarshal([]byte(resp), &prob); err != nil {
		return fmt.Errorf("failed to unmarshal the rfc7807 response: %w", err)
	}
	return &prob
}

// newProblemID will generate a ProblemID struct for use with VProblem
func newProblemID(errType, title string, status int) ProblemID {
	return ProblemID{
		Type:   errType,
		Title:  title,
		Status: status,
	}
}

// WithDetail will set the detail field in the VProblem
func (v *VProblem) WithDetail(d string) *VProblem {
	v.Detail = d
	return v
}

// WithHost will set the originating host in the VPrbolem. h can be a host name
// or IP.
func (v *VProblem) WithHost(h string) *VProblem {
	v.Host = h
	return v
}

// IsInstanceOf returns true if the VProblem is an occurrence of the given
// problem ID.
func (v *VProblem) IsInstanceOf(id ProblemID) bool {
	return v.Title == id.Title
}

// SendError will write an error response for the problem
func (v *VProblem) SendError(w http.ResponseWriter) {
	respBytes, err := json.Marshal(v)
	// If we can't convert the problem into JSON, fall back to plain old text so
	// that we have something to return. This signifies a programming error
	// though.
	if err != nil {
		errMsg := fmt.Sprintf("Failed to marshal problem details. Falling back to plaintext error return: %s", v.Detail)
		http.Error(w, errMsg, v.Status)
		return
	}
	w.Header().Set("Content-Type", ContentType)
	w.WriteHeader(v.Status)
	fmt.Fprintln(w, string(respBytes))
}

func MakeProblem(problemID ProblemID, detail string) Problem {
	hostname, _ := os.Hostname()

	return New(problemID).
		WithDetail(detail).
		WithHost(hostname)
}
