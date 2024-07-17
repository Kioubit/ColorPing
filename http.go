package main

import (
	"embed"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed template.html
var embedFS embed.FS
var htmlTemplate *template.Template

type clientState int

const (
	INITIAL = iota
	ACTIVE
)

type client struct {
	channel chan string
	state   clientState
}

var (
	clients     = make([]*client, 0)
	clientMutex sync.Mutex
)

func deleteClient(client *client) {
	for i := 0; i < len(clients); i++ {
		if clients[i] == client {
			clients[i] = clients[len(clients)-1]
			clients = clients[:len(clients)-1]
			return
		}
	}
}

func httpServer() error {
	htmlTemplate = template.Must(template.ParseFS(embedFS, "template.html"))
	http.HandleFunc("/stream", stream)
	http.HandleFunc("/", serveRoot)
	return http.ListenAndServe(":9090", nil)
}

func getInterfaceBaseIP() string {
	iFace, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return ""
	}
	addresses, err := iFace.Addrs()
	if err != nil {
		return ""
	}
	gua := ""
	ula := ""
	for _, v := range addresses {
		addr := v.String()
		if !strings.Contains(addr, ":") {
			continue
		}
		_, anet, err := net.ParseCIDR(addr)
		if err != nil {
			continue
		}
		if anet.IP.IsLinkLocalUnicast() {
			continue
		}
		if anet.IP.IsGlobalUnicast() {
			gua = strings.Split(anet.String(), "/")[0]
		}
		if anet.IP.IsPrivate() {
			ula = strings.Split(anet.String(), "/")[0]
		}
	}
	if gua != "" {
		return gua
	} else {
		return ula
	}
}

func serveRoot(w http.ResponseWriter, r *http.Request) {
	if r.RequestURI != "/" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	type pageData struct {
		BaseIP       string
		CanvasWidth  int
		CanvasHeight int
	}
	baseIP := getInterfaceBaseIP()
	if len(baseIP) == 21 {
		baseIP = strings.TrimSuffix(baseIP, ":")
	}
	err := htmlTemplate.Execute(w, pageData{
		BaseIP:       baseIP,
		CanvasHeight: 512,
		CanvasWidth:  512,
	})
	if err != nil {
		fmt.Println("Error executing HTML template:", err)
	}
}

var streamServerRunning atomic.Bool

func streamServer() {
	if !streamServerRunning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		for {
			clientMutex.Lock()
			if len(clients) == 0 {
				streamServerRunning.Store(false)
				clientMutex.Unlock()
				return
			}

			requiresInitial := false
			requiresUpdate := false
			for _, v := range clients {
				if v.state == INITIAL {
					requiresInitial = true
				} else {
					requiresUpdate = true
				}
				if requiresInitial && requiresUpdate {
					break
				}
			}

			dataInitial, dataUpdate := getPicture(requiresInitial, requiresUpdate)
			tmp := clients[:0]
			for _, v := range clients {
				if v.state == INITIAL {
					v.state = ACTIVE
					select {
					case v.channel <- dataInitial:
					default:
					}
				} else {
					if dataUpdate != "0" {
						select {
						case v.channel <- dataUpdate:
						default:
							// Client cannot keep up
							close(v.channel)
							continue
						}
					}
				}
				tmp = append(tmp, v)
			}
			clients = tmp
			clientMutex.Unlock()
			time.Sleep(500 * time.Millisecond)
		}
	}()
}

func stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}
	streamServer()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	messageChan := make(chan string, 40)
	newClient := &client{
		channel: messageChan,
		state:   INITIAL,
	}
	clientMutex.Lock()
	clients = append(clients, newClient)
	clientMutex.Unlock()

	// For when clients are removed prior to connection closed, to avoid a call to delete(clients, id)
	var channelClosedFirst = false
	go func() {
		// Listen for connection close
		<-r.Context().Done()
		clientMutex.Lock()
		if !channelClosedFirst {
			deleteClient(newClient)
		}
		close(messageChan)
		clientMutex.Unlock()
	}()

	for {
		data, ok := <-messageChan
		if !ok {
			channelClosedFirst = true
			return
		}
		_, _ = w.Write([]byte(data))
		flusher.Flush()
	}
}
