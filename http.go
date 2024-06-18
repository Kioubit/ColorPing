package main

import (
	"embed"
	"html/template"
	"log"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

//go:embed template.html
var embedFS embed.FS
var htmlTemplate *template.Template

type clientState int

const (
	INITIAL = 0
	ACTIVE  = iota
)

type client struct {
	channel chan string
	state   clientState
}

var (
	clientCounter      uint32 = 0
	clientCounterMutex sync.Mutex

	clients     = make(map[uint32]*client)
	clientMutex sync.RWMutex
)

func getClientID() uint32 {
	clientCounterMutex.Lock()
	defer clientCounterMutex.Unlock()
	clientCounter++
	if clientCounter == math.MaxUint32 {
		clientCounter = 0
		clearClients()
	}
	return clientCounter
}

func clearClients() {
	clientMutex.Lock()
	defer clientMutex.Unlock()
	for _, c := range clients {
		close(c.channel)
	}
	clients = make(map[uint32]*client)
}

func httpServer() {
	var err error

	htmlTemplate = template.Must(template.ParseFS(embedFS, "template.html"))
	http.HandleFunc("/stream", stream)
	http.HandleFunc("/", serveRoot)
	err = http.ListenAndServe(":9090", nil)
	if err != nil {
		log.Fatalln(err)
	}
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
		w.WriteHeader(404)
		_, _ = w.Write([]byte("404 not found"))
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
		log.Println(err)
	}
}

func streamServer() {
	for {
		clientMutex.RLock()
		if len(clients) == 0 {
			for {
				if len(clients) == 0 {
					clientMutex.RUnlock()
					time.Sleep(1 * time.Second)
					clientMutex.RLock()
				} else {
					break
				}
			}
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

		for clientID, v := range clients {
			if v.state == INITIAL {
				v.state = ACTIVE
				select {
				case v.channel <- dataInitial:
				default:
					continue
				}
			} else {
				if dataUpdate != "0" {
					select {
					case v.channel <- dataUpdate:
					default:
						// Client cannot keep up
						close(v.channel)
						delete(clients, clientID)
						continue
					}
				}
			}
		}
		clientMutex.RUnlock()
		time.Sleep(500 * time.Millisecond)
	}
}

func stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	messageChan := make(chan string, 40)
	id := getClientID()
	newClient := client{
		channel: messageChan,
		state:   INITIAL,
	}
	clientMutex.Lock()
	clients[id] = &newClient
	clientMutex.Unlock()

	// For when clients are removed prior to connection closed, to avoid a call to delete(clients, id)
	var channelClosedFirst = false
	go func() {
		// Listen for connection close
		<-r.Context().Done()
		clientMutex.Lock()
		if !channelClosedFirst {
			delete(clients, id)
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
