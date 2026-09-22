package main

import "testing"

func TestAdd(t *testing.T) {
	if add(20, 22) != 42 {
		t.Fatal("unexpected result")
	}
}
