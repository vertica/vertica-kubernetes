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

package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/vertica/vcluster/commands"
)

func main() {
	// this channel is used to handle the ctrl-c signal
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)

	// handle ctrl-c signal and reset cursor
	go func() {
		<-sigs

		fmt.Println("\n\nCtrl-C received, exiting.")
		fmt.Print("\x1b[?25h") // show cursor back

		os.Exit(0)
	}()

	commands.Execute()
}
