/*
 (c) Copyright [2021-2024] Open Text.
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

package replicationstatus

// Parms holds all of the option for a replication start invocation.
type Parms struct {
	TargetIP       string
	TargetDBName   string
	TargetUserName string
	TargetPassword string
	TransactionID  int64
}

type Option func(*Parms)

// Make will fill in the Parms based on the options chosen
func (s *Parms) Make(opts ...Option) {
	for _, opt := range opts {
		opt(s)
	}
}

func WithTargetIP(targetIP string) Option {
	return func(s *Parms) {
		s.TargetIP = targetIP
	}
}

func WithTargetDBName(targetDBName string) Option {
	return func(s *Parms) {
		s.TargetDBName = targetDBName
	}
}

func WithTargetUserName(targetUserName string) Option {
	return func(s *Parms) {
		s.TargetUserName = targetUserName
	}
}

func WithTargetPassword(targetPassword string) Option {
	return func(s *Parms) {
		s.TargetPassword = targetPassword
	}
}

func WithTransactionID(transactionID int64) Option {
	return func(s *Parms) {
		s.TransactionID = transactionID
	}
}
