package procx

import (
	"encoding/binary"
	"reflect"
	"testing"
)

func TestParseProcessArgs(t *testing.T) {
	args := make([]byte, 4)
	binary.NativeEndian.PutUint32(args, 3)
	args = append(args, []byte("/bin/sh\x00\x00\x00sh\x00-c\x00PACTIFY_JOB=argument\x00PATH=/bin\x00PACTIFY_JOB=target\x00\x00")...)
	got, err := parseProcessArgs(args)
	if err != nil || !reflect.DeepEqual(got, []string{"PATH=/bin", "PACTIFY_JOB=target"}) {
		t.Fatalf("environment=%q err=%v", got, err)
	}
	for _, bad := range [][]byte{nil, {1, 0, 0}, args[:7], args[:12], args[:len(args)-2]} {
		if _, err := parseProcessArgs(bad); err == nil {
			t.Errorf("accepted truncated procargs %q", bad)
		}
	}
	args = make([]byte, 4)
	binary.NativeEndian.PutUint32(args, 2)
	args = append(args, []byte("/bin/sh\x00\x00sh\x00\x00PACTIFY_JOB=target\x00")...)
	if got, err := parseProcessArgs(args); err != nil || !reflect.DeepEqual(got, []string{"PACTIFY_JOB=target"}) {
		t.Fatalf("empty argument confused with environment: %q %v", got, err)
	}
}
