package main

import "testing"

func Test_NeedWakeUp(t *testing.T) {
	if needWakeUp("wake_up") == true {
		t.Error("Expected false, got ", true)
	}
	if needWakeUp("pair") == true {
		t.Error("Expected false, got ", true)
	}
}
