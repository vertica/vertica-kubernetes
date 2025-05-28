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

package dropdb

// Parms holds all of the option for an invocation to drop the database in
// communal storage.
type Parms struct {
	Hosts  []Host
	DBName string
}

type Host struct {
	VNode string // Vertica node name in the format of v_<db>_node####
	IP    string // Current IP address of this host/pod
}

type Option func(*Parms)

// Make will fill in the Parms based on the options chosen
func (s *Parms) Make(opts ...Option) {
	for _, opt := range opts {
		opt(s)
	}
}

func WithHost(vnode, ip string) Option {
	return func(s *Parms) {
		if s.Hosts == nil {
			s.Hosts = make([]Host, 0)
		}
		s.Hosts = append(s.Hosts, Host{
			VNode: vnode,
			IP:    ip,
		})
	}
}

func WithDBName(name string) Option {
	return func(s *Parms) {
		s.DBName = name
	}
}
