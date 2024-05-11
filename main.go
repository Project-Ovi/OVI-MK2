package main

import (
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"gocv.io/x/gocv"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type data struct {
	prefix  string
	content string
}

var globalCollectedData []data
var img gocv.Mat
var cam *gocv.VideoCapture

func findGreen(img gocv.Mat, min_points int) (gocv.Mat, int, int) {
	runtime.LockOSThread()

	// Convert to HLS
	gocv.CvtColor(img, &img, gocv.ColorBGRToHLS)

	// Apply mask
	lower_bound := gocv.NewMatWithSizeFromScalar(gocv.NewScalar(85.0/2, 0.0, 10.0/100*255, 0.0), img.Rows(), img.Cols(), gocv.MatTypeCV8UC3)
	upper_bound := gocv.NewMatWithSizeFromScalar(gocv.NewScalar(150.0/2, 70.0/100*255, 255.0, 0.0), img.Rows(), img.Cols(), gocv.MatTypeCV8UC3)
	mask := gocv.NewMat()
	gocv.InRange(img, lower_bound, upper_bound, &mask)
	removedMask := gocv.NewMat()
	gocv.Merge([]gocv.Mat{mask, mask, mask}, &removedMask)
	gocv.BitwiseAnd(img, removedMask, &img)

	// Convert gray
	gocv.CvtColor(removedMask, &removedMask, gocv.ColorHLSToBGR)
	gocv.CvtColor(removedMask, &removedMask, gocv.ColorBGRToGray)

	// Apply gaussian blur
	gocv.GaussianBlur(removedMask, &removedMask, image.Pt(15, 15), 0, 0, gocv.BorderDefault)

	// Remove aberations
	gocv.Threshold(removedMask, &removedMask, 200.0, 255.0, gocv.ThresholdBinary)

	// Find contours
	contours := gocv.FindContours(removedMask, gocv.RetrievalCComp, gocv.ChainApproxNone)

	conts_points := contours.ToPoints()
	if len(conts_points) <= 0 {
		lower_bound.Close()
		upper_bound.Close()
		removedMask.Close()
		mask.Close()
		contours.Close()
		return img, -1, -1
	}

	// Find biggest contour
	max_index := 0
	for i, val := range conts_points {
		if len(conts_points[max_index]) < len(val) {
			max_index = i
		}
	}

	if len(conts_points[max_index]) < min_points {
		lower_bound.Close()
		upper_bound.Close()
		removedMask.Close()
		mask.Close()
		contours.Close()
		return img, -1, -1
	}

	// Find center
	Cx, Cy := 0, 0
	for _, val := range conts_points[max_index] {
		Cx += val.X
		Cy += val.Y
	}
	Cx /= len(conts_points[max_index])
	Cy /= len(conts_points[max_index])

	// Draw center
	gocv.Circle(&img, image.Pt(Cx, Cy), 1, color.RGBA{R: 0, G: 0, B: 255, A: 255}, 50)
	gocv.DrawContours(&img, contours, max_index, color.RGBA{R: 255, G: 0, B: 0, A: 255}, 20)

	lower_bound.Close()
	upper_bound.Close()
	removedMask.Close()
	mask.Close()
	contours.Close()

	return img, Cx, Cy
}

func findCameras() []string {
	out, err := exec.Command("v4l2-ctl", "--list-devices").Output()
	if err != nil {
		log.Println("Error executing command:")
		log.Println(err)
		return nil
	}
	text := string(out)
	var cameras []string
	for _, val := range strings.Split(text, "\n") {
		if len(val) < 1 {
			continue
		}
		if val[0] != '\t' {
			cameras = append(cameras, strings.ReplaceAll(val, ":", ""))
		}
	}

	return cameras
}

func handleHTTP() {
	// Find working directory
	WD, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	// Create a file server for serving static files
	fileServer := http.FileServer(http.Dir("static"))

	// Handle requests for static files
	http.Handle("/files/", http.StripPrefix("/files/", fileServer))

	// Handle root home page
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		raw, err := os.ReadFile(path.Join(WD, "/static/root.html"))
		if err != nil {
			fmt.Fprint(w, err)
			return
		}
		html := string(raw)

		fmt.Fprint(w, html)
	})

	// Handle websocket
	http.HandleFunc("/ws", handleWebSocket)

	// Run server
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Error upgrading to WebSocket:")
		log.Println(err)
		return
	}
	defer conn.Close()

	log.Printf("New connection from %s\n", conn.RemoteAddr().String())

	go func() {
		for {
			_, rawmsg, err := conn.ReadMessage()
			if err != nil {
				log.Println(err)
				return
			}
			msg := string(rawmsg)

			switch msg[:3] {
			case "CAM":
				globalCollectedData[5].content = msg[3:]

			case "MAN":
				globalCollectedData[4].content = msg[3:]

			case "CTR":
				fmt.Println(msg[3:])
			}
		}
	}()

	// Send data
	for {
		// Send all
		for _, val := range globalCollectedData {

			err := conn.WriteMessage(websocket.TextMessage, []byte(val.prefix+val.content))
			if err != nil {
				log.Println("Failed to send data with prefix", val.prefix)
				log.Println(err)

				err := conn.Close()
				if err != nil {
					log.Println("Failed to close connection:")
					log.Println(err)
				}
				log.Println("Closed websocket connection")
				return
			}

			time.Sleep(5 * time.Millisecond)
		}
	}
}

func collectData() {
	// Set defaults
	globalCollectedData[0].prefix = "CAM"
	globalCollectedData[1].prefix = "CAS"
	globalCollectedData[2].prefix = "CXD"
	globalCollectedData[3].prefix = "CYD"
	globalCollectedData[4].prefix = "MAN"
	globalCollectedData[4] = data{
		prefix:  "MAN",
		content: "0",
	}
	globalCollectedData[5] = data{
		prefix:  "CON",
		content: "0",
	}

	prevCamera := "-1"
	for {
		// Collect camera image and Cx, Cy
		func() {
			// Check camera
			if globalCollectedData[5].content != prevCamera && globalCollectedData[5].content != "-1" {
				// Find ID
				id, err := strconv.Atoi(globalCollectedData[5].content)
				if err != nil {
					log.Println("Failed to parse cam ID:")
					log.Println(err)
					return
				}

				// Open camera at ID
				cam, err = gocv.VideoCaptureDevice(id)
				if err != nil {
					if id >= len(findCameras()) {
						log.Println("Camera index out of range:")
						log.Println(err)
						return
					}
					log.Printf("Failed to open camera %s:\n", findCameras()[id])
					log.Println(err)
					return
				}

				// Set parameters for the opened camera
				cam.Set(gocv.VideoCaptureFPS, 60)

				// Update prev variable
				prevCamera = globalCollectedData[5].content

			} else if globalCollectedData[5].content == "-1" {
				return
			}

			// Read camera
			temp := gocv.NewMat()
			if !cam.Read(&temp) {
				log.Println("Failed to read webcam")
				return
			}

			// Convert img
			Cx, Cy := -1, -1
			if globalCollectedData[4].content == "0" {
				_, Cx, Cy = findGreen(temp, 100)
			}

			// Replace original img
			temp.CopyTo(&img)
			temp.Close()

			if img.Empty() {
				fmt.Println("Empty image")
				return
			}

			// Encode image
			buff, err := gocv.IMEncode(gocv.PNGFileExt, img)
			if err != nil {
				log.Println("Failed to encode image:")
				log.Println(err)
				return
			}

			// Convert to base64
			encodedIMG := base64.StdEncoding.EncodeToString(buff.GetBytes())

			// Save data
			globalCollectedData[0].content = encodedIMG
			globalCollectedData[2].content = fmt.Sprint(Cx)
			globalCollectedData[3].content = fmt.Sprint(Cy)
		}()

		// Collect webcams
		func() {
			globalCollectedData[1].content = strings.Join(findCameras(), "|")
		}()
	}
}

func main() {
	// Initialize data
	globalCollectedData = make([]data, 20)
	img = gocv.NewMat()

	// Start webserver
	go handleHTTP()

	// Calculate movements
	go func() {
		window := gocv.NewWindow("Hello")

		for {

			if img.Empty() {
				continue
			}

			window.IMShow(img)
			window.WaitKey(1)
		}
	}()

	// Start collecting data
	collectData()
}
