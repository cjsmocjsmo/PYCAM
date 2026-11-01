package main

import (
	"bytes"
	"os"
	"testing"
)

func TestPacketizer(t *testing.T) {
	raw, err := os.ReadFile("testdata/sample.h264")
	if err != nil {
		t.Fatal(err)
	}
	var codec string
	var initSeg []byte
	var frags [][]byte
	var keys []bool
	p := NewPacketizer(60, 6,
		func(c string, d []byte) { codec, initSeg = c, d },
		func(d []byte, k bool) { frags = append(frags, d); keys = append(keys, k) })

	// Odd-sized chunks exercise start-code splitting across writes.
	for i := 0; i < len(raw); i += 997 {
		p.Write(raw[i:min(i+997, len(raw))])
	}

	if codec == "" || !bytes.Contains(initSeg, []byte("avcC")) {
		t.Fatalf("missing init: codec=%q", codec)
	}
	if len(frags) < 8 || !keys[0] {
		t.Fatalf("frags=%d firstKey=%v", len(frags), keys[0])
	}
	for _, f := range frags {
		if !bytes.Contains(f, []byte("moof")) || !bytes.Contains(f, []byte("mdat")) {
			t.Fatal("fragment missing moof/mdat")
		}
	}
	if out := os.Getenv("PACKETIZER_OUT"); out != "" {
		all := append([]byte(nil), initSeg...)
		for _, f := range frags {
			all = append(all, f...)
		}
		os.WriteFile(out, all, 0o644)
	}
}
