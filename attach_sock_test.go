package main

import "testing"

func TestExtractSockFlag(t *testing.T) {
	cases := []struct {
		args     []string
		sock, id string
	}{
		{[]string{"--sock", "/tmp/x.sock"}, "/tmp/x.sock", ""},
		{[]string{"--sock", "/tmp/x.sock", "s3"}, "/tmp/x.sock", "s3"},
		{[]string{"s3", "--sock", "/tmp/x.sock"}, "/tmp/x.sock", "s3"},
		{[]string{"--sock=/tmp/y.sock"}, "/tmp/y.sock", ""},
		{[]string{"s5"}, "", "s5"},
		{[]string{}, "", ""},
	}
	for _, c := range cases {
		sock, id := extractSockFlag(c.args)
		if sock != c.sock || id != c.id {
			t.Errorf("extractSockFlag(%v) = (%q,%q), want (%q,%q)", c.args, sock, id, c.sock, c.id)
		}
	}
}
