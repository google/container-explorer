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
	"bytes"
	"context"
	"testing"
)

func TestOSRunner(t *testing.T) {
	runner := &osRunner{}
	ctx := context.Background()

	// Test Run
	out, err := runner.Run(ctx, "echo", "hello")
	if err != nil {
		t.Errorf("Run failed: %v", err)
	}
	if string(out) != "hello\n" {
		t.Errorf("expected 'hello\\n', got %q", string(out))
	}

	// Test RunWithoutContext
	out, err = runner.RunWithoutContext("echo", "world")
	if err != nil {
		t.Errorf("RunWithoutContext failed: %v", err)
	}
	if string(out) != "world\n" {
		t.Errorf("expected 'world\\n', got %q", string(out))
	}

	// Test RunSeparate
	var stdout, stderr bytes.Buffer
	err = runner.RunSeparate(ctx, "echo", []string{"separate"}, &stdout, &stderr)
	if err != nil {
		t.Errorf("RunSeparate failed: %v", err)
	}
	if stdout.String() != "separate\n" {
		t.Errorf("expected stdout 'separate\\n', got %q", stdout.String())
	}
}
