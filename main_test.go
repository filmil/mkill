package main

import "testing"

func TestTrivial(t *testing.T) {
	// A trivial test to ensure the test runner executes successfully.
	if 1+1 != 2 {
		t.Error("Math is broken")
	}
}
