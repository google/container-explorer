/*
Copyright 2026 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"context"
	"io"
	"os/exec"
)

// CommandRunner defines the interface for executing system commands.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
	RunSeparate(ctx context.Context, name string, args []string, stdout, stderr io.Writer) error
	RunWithoutContext(name string, args ...string) ([]byte, error)
}

// osRunner executes actual system commands.
type osRunner struct{}

// Run executes a command with context and returns combined output.
func (r *osRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

// RunSeparate executes a command with context, redirecting stdout and stderr.
func (r *osRunner) RunSeparate(ctx context.Context, name string, args []string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// RunWithoutContext executes a command without context and returns combined output.
func (r *osRunner) RunWithoutContext(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

// Runner is the active command runner instance.
var Runner CommandRunner = &osRunner{}
