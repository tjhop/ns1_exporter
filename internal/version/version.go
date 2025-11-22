// Copyright 2023 TJ Hoplock
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package version

import (
	"fmt"
	"runtime"
)

var (
	Version   string // will be populated by linker during build
	BuildDate string // will be populated by linker during build
	Commit    string // will be populated by linker during build
)

// Print outputs human readable build about the binary to stdout.
// Models return on: github.com/prometheus/common/version.Print().
func Print(programName string) string {
	return fmt.Sprintf("%s build info:\n\tversion: %s\n\tbuild date: %s\n\tcommit: %s\n\tgo version: %s\n",
		programName,
		Version,
		BuildDate,
		Commit,
		runtime.Version(),
	)
}

// Info print build info in a more condensed, single line format.
// Models return on: github.com/prometheus/common/version.Info().
func Info() string {
	return fmt.Sprintf("(version=%s, build_date=%s, commit=%s)", Version, BuildDate, Commit)
}
