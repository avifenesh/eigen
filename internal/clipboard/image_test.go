package clipboard

import "testing"

func TestContainsLine(t *testing.T) {
	data := []byte("text/plain\nimage/png\nimage/jpeg")
	if !containsLine(data, "image/png") {
		t.Error("should find image/png")
	}
	if !containsLine(data, "image/jpeg") {
		t.Error("should find trailing image/jpeg")
	}
	if !containsLine(data, "text/plain") {
		t.Error("should find leading text/plain")
	}
	if containsLine(data, "image/gif") {
		t.Error("should not find absent type")
	}
	if containsLine(data, "image/pn") {
		t.Error("must match whole lines, not substrings")
	}
}
