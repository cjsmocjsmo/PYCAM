package main

import (
	"context"
	"log"
	"net"
	"sync"
	"time"
)

// Message is one unit sent to a viewer. Exactly one of Codec/Data semantics applies:
// Init messages carry Codec + init segment; others carry a media fragment.
type Message struct {
	Init  bool
	Codec string
	Data  []byte
	Key   bool
}

type Viewer struct {
	C chan Message
}

// Hub owns the single TCP connection to one camera and fans fragments out to viewers.
// The camera connection exists only while at least one viewer is watching.
type Hub struct {
	Name string
	Addr string

	fps, framesPerFrag int
	idleGrace          time.Duration

	mu        sync.Mutex
	viewers   map[*Viewer]struct{}
	initMsg   *Message
	connected bool
	cancel    context.CancelFunc
	stopTimer *time.Timer
}

func NewHub(name, addr string, fps, framesPerFrag int, idleGrace time.Duration) *Hub {
	return &Hub{
		Name: name, Addr: addr,
		fps: fps, framesPerFrag: framesPerFrag, idleGrace: idleGrace,
		viewers: map[*Viewer]struct{}{},
	}
}

// Subscribe registers a viewer, starting the camera connection if needed.
func (h *Hub) Subscribe() *Viewer {
	v := &Viewer{C: make(chan Message, 32)}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.viewers[v] = struct{}{}
	if h.stopTimer != nil {
		h.stopTimer.Stop()
		h.stopTimer = nil
	}
	if h.initMsg != nil {
		v.C <- *h.initMsg
	}
	if h.cancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		h.cancel = cancel
		go h.run(ctx)
	}
	return v
}

func (h *Hub) Unsubscribe(v *Viewer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.viewers[v]; !ok {
		return
	}
	delete(h.viewers, v)
	if len(h.viewers) == 0 && h.cancel != nil && h.stopTimer == nil {
		h.stopTimer = time.AfterFunc(h.idleGrace, h.stopIfIdle)
	}
}

func (h *Hub) stopIfIdle() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.viewers) == 0 && h.cancel != nil {
		h.cancel()
		h.cancel = nil
		h.stopTimer = nil
	}
}

// Status reports connection state for the UI.
func (h *Hub) Status() (connected bool, viewers int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.connected, len(h.viewers)
}

func (h *Hub) setConnected(c bool) {
	h.mu.Lock()
	h.connected = c
	h.mu.Unlock()
}

func (h *Hub) broadcast(m Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m.Init {
		h.initMsg = &m
	}
	for v := range h.viewers {
		select {
		case v.C <- m:
		default:
			// Too slow: drop the viewer; the browser reconnects and resyncs on a keyframe.
			delete(h.viewers, v)
			close(v.C)
		}
	}
}

func (h *Hub) run(ctx context.Context) {
	for ctx.Err() == nil {
		conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", h.Addr)
		if err != nil {
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
			continue
		}
		log.Printf("%s: connected to %s", h.Name, h.Addr)
		h.setConnected(true)
		h.pump(ctx, conn)
		h.setConnected(false)
		log.Printf("%s: disconnected", h.Name)
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}

func (h *Hub) pump(ctx context.Context, conn net.Conn) {
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()
	defer conn.Close()

	pk := NewPacketizer(h.fps, h.framesPerFrag,
		func(codec string, data []byte) { h.broadcast(Message{Init: true, Codec: codec, Data: data}) },
		func(data []byte, key bool) { h.broadcast(Message{Data: data, Key: key}) })
	buf := make([]byte, 64*1024)
	for {
		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		n, err := conn.Read(buf)
		if n > 0 {
			pk.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}
