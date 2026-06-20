package gui

import (
	"testing"
)

func TestServiceValidationErrors(t *testing.T) {
	svc := NewService(nil)
	if _, err := svc.State(""); err == nil || err.Error() != "session id required" {
		t.Fatalf("State should validate id, got %v", err)
	}
	if _, err := svc.Input("", "hello"); err == nil || err.Error() != "session id required" {
		t.Fatalf("Input should validate id, got %v", err)
	}
	if _, err := svc.Input("s1", "   "); err == nil || err.Error() != "input text required" {
		t.Fatalf("Input should validate text, got %v", err)
	}
	if _, _, err := svc.Events(nil, ""); err == nil || err.Error() != "session id required" {
		t.Fatalf("Events should validate id, got %v", err)
	}
}
