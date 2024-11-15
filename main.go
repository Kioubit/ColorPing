package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"github.com/songgao/water"
	"image"
	"image/color"
	"image/png"
	"os"
	"runtime"
	"sync"
)

const interfaceName = "canvas"

func main() {
	prePopulatePixelArray()
	packetChan := make(chan *[]byte, 1000)
	for i := 0; i < runtime.NumCPU(); i++ {
		go packetHandler(packetChan)
	}
	go func() {
		err := startInterface(packetChan)
		if err != nil {
			fmt.Println("Interface handler error:", err)
			os.Exit(0)
		}
	}()
	fmt.Println("Kioubit ColorPing started")
	fmt.Println("Interface name:", interfaceName, "HTTP server port: 9090")
	if err := httpServer(); err != nil {
		fmt.Println("Error starting HTTP server:", err)
		return
	}
}

func prePopulatePixelArray() {
	for x := 0; x < len(pixelArray); x++ {
		for y := 0; y < len(pixelArray[x]); y++ {
			pixelArray[x][y] = &pixel{
				r: uint8(0),
				g: uint8(0),
				b: uint8(0),
			}
		}
	}
}

var pktPool = sync.Pool{
	New: func() interface{} { return make([]byte, 2000) },
}

func startInterface(packetChan chan *[]byte) error {
	config := water.Config{
		DeviceType: water.TUN,
	}
	config.Name = interfaceName
	iFace, err := water.New(config)
	if err != nil {
		return err
	}

	for {
		packet := pktPool.Get().([]byte)
		n, err := iFace.Read(packet)
		if err != nil {
			return err
		}
		packet = packet[:n]
		packetChan <- &packet
	}
}

func packetHandler(packetChan chan *[]byte) {
	for {
		packet := <-packetChan
		if len(*packet) < 40 {
			continue
		}
		if (*packet)[0] != 0x60 {
			continue
		}
		destinationAddress := (*packet)[24:40]
		relevant := destinationAddress[8:]
		// FORMAT: XXXX:YYYY:11RR:GGBB
		x := binary.BigEndian.Uint16(relevant[0:2])
		y := binary.BigEndian.Uint16(relevant[2:4])

		if relevant[4] != 0x11 {
			continue
		}

		r := relevant[5]
		g := relevant[6]
		b := relevant[7]

		if x > 512 || y > 512 {
			continue
		}

		obj := pixelArray[x][y]

		obj.Lock()
		if obj.r != r || obj.g != g || obj.b != b {
			obj.r = r
			obj.g = g
			obj.b = b
			obj.changed = true
		}
		obj.Unlock()
		pktPool.Put(packet)
	}

}

type pixel struct {
	sync.Mutex
	r       uint8
	g       uint8
	b       uint8
	changed bool
}

// 0 - 513
var pixelArray [513][513]*pixel

func getPicture(fullUpdate bool, incrementalUpdate bool) (string, string) {
	anyChange := false
	canvasFullUpdate := image.NewRGBA(image.Rect(0, 0, 512, 512))
	canvasIncrementalUpdate := image.NewRGBA(image.Rect(0, 0, 512, 512))

	for x := 0; x < len(pixelArray); x++ {
		for y := 0; y < len(pixelArray[x]); y++ {
			obj := pixelArray[x][y]
			obj.Lock()
			var newColor *color.RGBA
			if incrementalUpdate {
				if obj.changed {
					newColor = &color.RGBA{
						R: obj.r,
						G: obj.g,
						B: obj.b,
						A: 255,
					}
					obj.changed = false
					anyChange = true
					canvasIncrementalUpdate.SetRGBA(x, y, *newColor)
				} else if !fullUpdate {
					obj.Unlock()
					continue
				}
			}
			if newColor == nil {
				newColor = &color.RGBA{
					R: obj.r,
					G: obj.g,
					B: obj.b,
					A: 255,
				}
			}
			obj.Unlock()
			canvasFullUpdate.SetRGBA(x, y, *newColor)
		}
	}

	encoder := png.Encoder{
		CompressionLevel: png.BestSpeed,
	}

	incrementalUpdateResult := "0"
	if anyChange {
		buff := new(bytes.Buffer)
		err := encoder.Encode(buff, canvasIncrementalUpdate)
		if err != nil {
			fmt.Println("PNG encoding error:", err)
		}
		incrementalUpdateResult = "event: u\ndata:" + base64.StdEncoding.EncodeToString(buff.Bytes()) + "\n\n"
	}

	fullUpdateResult := "0"
	if fullUpdate {
		buff := new(bytes.Buffer)
		err := encoder.Encode(buff, canvasFullUpdate)
		if err != nil {
			fmt.Println("PNG encoding error:", err)
		}
		fullUpdateResult = "event: u\ndata:" + base64.StdEncoding.EncodeToString(buff.Bytes()) + "\n\n"
	}

	return fullUpdateResult, incrementalUpdateResult
}
