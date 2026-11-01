package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/Eyevinn/mp4ff/mp4"
)

const (
	nalSlice = 1
	nalIDR   = 5
	nalSPS   = 7
	nalPPS   = 8

	timescale = 90000
	trackID   = 1
)

var startCode = []byte{0, 0, 1}

// Packetizer converts a raw Annex-B H.264 byte stream into fragmented MP4.
// It assumes one slice NAL per frame, which is what the Pi hardware encoder emits.
type Packetizer struct {
	frameDur  uint32
	perFrag   int
	onInit    func(codec string, data []byte)
	onFrag    func(data []byte, key bool)
	buf       []byte
	scanFrom  int
	sps, pps  []byte
	haveInit  bool
	seq       uint32
	decodeTS  uint64
	samples   []mp4.FullSample
	fragIsKey bool
	synced    bool
}

func NewPacketizer(fps, framesPerFragment int,
	onInit func(string, []byte), onFrag func([]byte, bool)) *Packetizer {
	return &Packetizer{
		frameDur: uint32(timescale / fps),
		perFrag:  framesPerFragment,
		onInit:   onInit,
		onFrag:   onFrag,
	}
}

// Write feeds raw stream bytes; complete NAL units are processed immediately.
func (p *Packetizer) Write(data []byte) {
	p.buf = append(p.buf, data...)
	var starts []int
	for pos := p.scanFrom; ; {
		i := bytes.Index(p.buf[pos:], startCode)
		if i < 0 {
			break
		}
		starts = append(starts, pos+i)
		pos += i + len(startCode)
	}
	if len(starts) == 0 {
		p.scanFrom = max(0, len(p.buf)-2)
		return
	}
	for i := 0; i+1 < len(starts); i++ {
		nal := bytes.TrimRight(p.buf[starts[i]+3:starts[i+1]], "\x00")
		p.handleNAL(nal)
	}
	last := starts[len(starts)-1]
	p.buf = append(p.buf[:0], p.buf[last:]...)
	p.scanFrom = 0
}

func (p *Packetizer) handleNAL(nal []byte) {
	if len(nal) == 0 {
		return
	}
	switch nal[0] & 0x1f {
	case nalSPS:
		p.setParam(&p.sps, nal)
	case nalPPS:
		p.setParam(&p.pps, nal)
	case nalIDR:
		p.flush()
		p.addFrame(nal, true)
	case nalSlice:
		p.addFrame(nal, false)
	}
}

func (p *Packetizer) setParam(dst *[]byte, nal []byte) {
	if bytes.Equal(*dst, nal) {
		return
	}
	*dst = append([]byte(nil), nal...)
	if p.sps == nil || p.pps == nil {
		return
	}
	init := mp4.CreateEmptyInit()
	trak := init.AddEmptyTrack(timescale, "video", "und")
	if err := trak.SetAVCDescriptor("avc1", [][]byte{p.sps}, [][]byte{p.pps}, true); err != nil {
		return
	}
	var w bytes.Buffer
	if err := init.Encode(&w); err != nil {
		return
	}
	p.haveInit = true
	codec := fmt.Sprintf("avc1.%02X%02X%02X", p.sps[1], p.sps[2], p.sps[3])
	p.onInit(codec, w.Bytes())
}

func (p *Packetizer) addFrame(nal []byte, key bool) {
	if !p.haveInit {
		return
	}
	if !p.synced && !key {
		return // wait for a keyframe after (re)start
	}
	p.synced = true
	if len(p.samples) == 0 {
		p.fragIsKey = key
	}
	avcc := make([]byte, 4+len(nal))
	binary.BigEndian.PutUint32(avcc, uint32(len(nal)))
	copy(avcc[4:], nal)
	flags := mp4.NonSyncSampleFlags
	if key {
		flags = mp4.SyncSampleFlags
	}
	p.samples = append(p.samples, mp4.FullSample{
		Sample:     mp4.Sample{Flags: flags, Dur: p.frameDur, Size: uint32(len(avcc))},
		DecodeTime: p.decodeTS,
		Data:       avcc,
	})
	p.decodeTS += uint64(p.frameDur)
	if len(p.samples) >= p.perFrag {
		p.flush()
	}
}

func (p *Packetizer) flush() {
	if len(p.samples) == 0 {
		return
	}
	p.seq++
	frag, err := mp4.CreateFragment(p.seq, trackID)
	if err == nil {
		for _, s := range p.samples {
			if err = frag.AddFullSampleToTrack(s, trackID); err != nil {
				break
			}
		}
	}
	key := p.fragIsKey
	p.samples = nil
	p.fragIsKey = false
	if err != nil {
		return
	}
	var w bytes.Buffer
	if err := frag.Encode(io.Writer(&w)); err != nil {
		return
	}
	p.onFrag(w.Bytes(), key)
}
