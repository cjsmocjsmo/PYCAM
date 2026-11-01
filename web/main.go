package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

//go:embed templates/index.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

type Stream struct {
	ID   int
	Name string
	Addr string
	WS   string
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 64 * 1024,
	CheckOrigin:     func(*http.Request) bool { return true },
}

func main() {
	listen := flag.String("listen", ":8080", "HTTP listen address")
	cam1 := flag.String("cam1", "10.0.4.60:9999", "camera 1 address")
	cam2 := flag.String("cam2", "10.0.4.67:9999", "camera 2 address")
	fps := flag.Int("fps", 60, "camera framerate (must match pycamserver.py)")
	frames := flag.Int("frames", 6, "frames per fMP4 fragment (latency vs overhead)")
	idle := flag.Duration("idle", 5*time.Second, "disconnect from cameras this long after last viewer leaves")
	flag.Parse()

	hubs := []*Hub{
		NewHub("cam1", *cam1, *fps, *frames, *idle),
		NewHub("cam2", *cam2, *fps, *frames, *idle),
	}
	streams := make([]Stream, len(hubs))
	for i, h := range hubs {
		streams[i] = Stream{ID: i + 1, Name: fmt.Sprintf("Camera %d", i+1), Addr: h.Addr, WS: fmt.Sprintf("/ws/%d", i+1)}
	}

	tmpl := template.Must(template.ParseFS(templateFS, "templates/index.html"))
	static, _ := fs.Sub(staticFS, "static")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if err := tmpl.Execute(w, streams); err != nil {
			log.Print(err)
		}
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /ws/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil || id < 1 || id > len(hubs) {
			http.NotFound(w, r)
			return
		}
		serveStream(hubs[id-1], w, r)
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		type st struct {
			ID        int  `json:"id"`
			Connected bool `json:"connected"`
			Viewers   int  `json:"viewers"`
		}
		out := make([]st, len(hubs))
		for i, h := range hubs {
			c, v := h.Status()
			out[i] = st{i + 1, c, v}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	log.Printf("listening on %s", *listen)
	log.Fatal(http.ListenAndServe(*listen, mux))
}

func serveStream(h *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	v := h.Subscribe()
	defer h.Unsubscribe(v)

	// Detect browser close.
	gone := make(chan struct{})
	go func() {
		defer close(gone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	gotKey := false
	for {
		select {
		case <-gone:
			return
		case m, ok := <-v.C:
			if !ok {
				return
			}
			if m.Init {
				gotKey = false
				if conn.WriteMessage(websocket.TextMessage, []byte(m.Codec)) != nil {
					return
				}
			} else {
				// Join the live stream only at a keyframe fragment.
				if !gotKey && !m.Key {
					continue
				}
				gotKey = true
			}
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if conn.WriteMessage(websocket.BinaryMessage, m.Data) != nil {
				return
			}
		}
	}
}
