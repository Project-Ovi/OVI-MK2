package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"log"
	"math"
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
	"gopkg.in/yaml.v3"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type data struct {
	prefix  string
	content string
}

type Calibrations struct {
	// Image settings
	Resolution_x int
	Resolution_y int
	Flip_x_axis  bool
	Flip_y_axis  bool

	// Detection rules
	Hue        [2]float64
	Saturation [2]float64
	Lightness  [2]float64

	// Sensitivity settings
	Center_offset_x int
	Center_offset_y int
	Max_deviation   float64

	// Networking settings
	Request_ip  string
	Webgui_port string

	// Debugging settings
	Live_reload     bool
	Reload_interval uint

	// Speed settings
	Rotation_speed  int
	Upwards_speed   int
	Extension_speed int

	// Limit settings
	Rotation_limit      int
	Rotation_revolution int
	Upwards_limit       int
	Extension_limit     int
	Manual_interval     float64
}

var Cx, Cy = -1, -1

var globalCollectedData []data
var calibrationData Calibrations
var calibrationsLoaded = false
var img gocv.Mat
var cam *gocv.VideoCapture

var cameraIDs []int

var HTTPclient = &http.Client{}

func findGreen(img gocv.Mat, min_points int) (gocv.Mat, int, int) {
	runtime.LockOSThread()

	// Convert to HLS
	gocv.CvtColor(img, &img, gocv.ColorBGRToHLS)

	// Apply mask
	lower_bound := gocv.NewMatWithSizeFromScalar(gocv.NewScalar(calibrationData.Hue[0]/2.0, calibrationData.Lightness[0]/100.0*255.0, calibrationData.Saturation[0]/100.0*255.0, 0.0), img.Rows(), img.Cols(), gocv.MatTypeCV8UC3)
	upper_bound := gocv.NewMatWithSizeFromScalar(gocv.NewScalar(calibrationData.Hue[1]/2.0, calibrationData.Lightness[1]/100.0*255.0, calibrationData.Saturation[1]/100.0*255.0, 0.0), img.Rows(), img.Cols(), gocv.MatTypeCV8UC3)
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
	if len(conts_points[max_index]) < 1 {
		return img, -1, -1
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
	lines := strings.Split(text, "\n")
	for i, val := range lines {
		if len(val) < 1 {
			continue
		}
		if val[0] != '\t' {
			cam := lines[i+1]
			id, err := strconv.Atoi(string(cam[len(cam)-1]))
			if err != nil {
				log.Println(err)
				continue
			}

			cameraIDs = append(cameraIDs, id)
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
	log.Println("Started webserver on port", calibrationData.Webgui_port)
	log.Fatal(http.ListenAndServe(calibrationData.Webgui_port, nil))
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
				globalCollectedData[6].content = msg[3:]
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
		content: "1",
	}
	globalCollectedData[5] = data{
		prefix:  "CON",
		content: "0",
	}
	globalCollectedData[6].prefix = "CTR"

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
				if prevCamera != "-1" {
					cam.Close()
				}
				cam, err = gocv.VideoCaptureDevice(cameraIDs[id])
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

			// Flip image
			if calibrationData.Flip_x_axis && calibrationData.Flip_y_axis {
				gocv.Flip(temp, &temp, -1)
			} else if calibrationData.Flip_x_axis {
				gocv.Flip(temp, &temp, 0)
			} else if calibrationData.Flip_y_axis {
				gocv.Flip(temp, &temp, 1)
			}

			// Convert img
			if globalCollectedData[4].content == "0" {
				_, Cx, Cy = findGreen(temp, 100)
			}

			// Convert Cx and Cy to the desired resolution
			if Cx != -1 {
				Cx = int(float64(Cx) / float64(temp.Cols()) * float64(calibrationData.Resolution_x))
				Cy = int(float64(Cy) / float64(temp.Rows()) * float64(calibrationData.Resolution_y))
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

func move(rotation int, up int, extension int, grip bool) error {
	// Make a request
	req, err := http.NewRequest(http.MethodPost, calibrationData.Request_ip, bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return err
	}

	// Set user agent
	req.Header.Set("User-Agent", "oviwebgui/2024.1.2")

	// Set rotation
	if rotation > 0 {
		req.Header.Set("R1", fmt.Sprint(rotation))
		req.Header.Set("R2", "0")
	} else if rotation < 0 {
		req.Header.Set("R1", "0")
		req.Header.Set("R2", fmt.Sprint(math.Abs(float64(rotation))))
	} else {
		req.Header.Set("R1", "0")
		req.Header.Set("R2", "0")
	}

	// Set upwards movement
	if up > 0 {
		req.Header.Set("U1", fmt.Sprint(up))
		req.Header.Set("U2", "0")
	} else if rotation < 0 {
		req.Header.Set("U1", "0")
		req.Header.Set("U2", fmt.Sprint(math.Abs(float64(up))))
	} else {
		req.Header.Set("U1", "0")
		req.Header.Set("U2", "0")
	}

	// Set extend
	if extension > 0 {
		req.Header.Set("E1", fmt.Sprint(extension))
		req.Header.Set("E2", "0")
	} else if rotation < 0 {
		req.Header.Set("E1", "0")
		req.Header.Set("E2", fmt.Sprint(math.Abs(float64(extension))))
	} else {
		req.Header.Set("E1", "0")
		req.Header.Set("E2", "0")
	}

	// Set gripper position
	if grip {
		req.Header.Set("G1", "255")
	} else {
		req.Header.Set("G1", "0")
	}

	// Make request
	resp, err := HTTPclient.Do(req)
	if err != nil {
		log.Println(err)
		return err
	}
	defer resp.Body.Close()

	// Error checking
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP POST request failed with code %d", resp.StatusCode)
	}

	return nil
}

func loadConfig() error {
	// Read calibrations file
	f, err := os.ReadFile("calibrations.yml")
	if err != nil {
		return err
	}

	// Unmarshall the file
	err = yaml.Unmarshal(f, &calibrationData)
	if err != nil {
		return err
	}
	calibrationsLoaded = true
	return nil
}

func loadConfigContinuous() {
	for {
		// Load config
		err := loadConfig()
		if err != nil {
			log.Println(err)
		}

		// If not continuous, break
		if !calibrationData.Live_reload {
			break
		}
		time.Sleep(time.Duration(calibrationData.Reload_interval) * time.Millisecond)
	}

	log.Println("Loaded calibrations in non-live mode")
}

func main() {
	// Initialize data
	findCameras()
	go loadConfigContinuous()
	globalCollectedData = make([]data, 20)
	img = gocv.NewMat()

	// Await initialization
	log.Println("Waiting for calibrations file to load")
	for !calibrationsLoaded {
		time.Sleep(time.Millisecond)
	}
	log.Println("Calibrations data loaded")

	fmt.Println(calibrationData)

	// Start webserver
	go handleHTTP()

	// Calculate movements
	go func() {
		log.Println("Started homing sequence")
		home()
		log.Println("Finished homing sequence")
		for {
			if globalCollectedData[4].content == "0" {
				autoRoam()
			} else {
				manulaRoam()
			}
		}
	}()

	// Prepare cleanup
	defer func() {
		cam.Close()
		img.Close()
	}()

	// Start collecting data
	collectData()
}

var rot_i, up_i, ext_i time.Duration

func home() {
	// Extend everything
	move(0, calibrationData.Upwards_speed, calibrationData.Extension_speed, true)
	time.Sleep(time.Duration(max(calibrationData.Upwards_limit, calibrationData.Extension_limit)) * time.Millisecond)

	// Move extension to half the position
	move(0, 0, -calibrationData.Extension_speed, true)
	time.Sleep(time.Duration(calibrationData.Extension_limit/2) * time.Millisecond)

	// End movement
	move(0, 0, 0, false)

	// Set vars
	up_i = time.Duration(calibrationData.Upwards_limit) * time.Millisecond
	ext_i = time.Duration(calibrationData.Extension_limit/2) * time.Millisecond
}

func autoRoam() {
	// Make sure we don't overrun a limit
	if int(rot_i.Milliseconds()) > calibrationData.Rotation_limit && calibrationData.Rotation_limit > 0 {
		move(-calibrationData.Rotation_speed, 0, 0, true)
		time.Sleep(time.Duration(calibrationData.Rotation_limit%calibrationData.Rotation_revolution) * time.Millisecond)
	}

	// Find center of image
	img_Cx := float64(img.Cols()) / 2.0
	img_Cy := float64(img.Rows()) / 2.0

	// Ofset center
	img_Cx += float64(calibrationData.Center_offset_x)
	img_Cy += float64(calibrationData.Center_offset_y)

	// Find distance
	dist := math.Sqrt(math.Pow(float64(Cx)-img_Cx, 2) + math.Pow(float64(Cy)-img_Cy, 2))

	if dist <= calibrationData.Max_deviation {
		// Object can be picked up
		// Drop down to object
		move(0, -calibrationData.Upwards_speed, 0, false)
		time.Sleep(up_i)

		// Grab object
		move(0, 0, 0, true)

		// Move up
		move(0, calibrationData.Upwards_speed, 0, true)
		time.Sleep(up_i)

		// Rotate to the start position
		move(-calibrationData.Rotation_speed, 0, 0, true)
		time.Sleep(time.Duration(rot_i.Milliseconds()/int64(calibrationData.Rotation_revolution)) * time.Millisecond)

		// Move to the limit of the extension
		move(0, 0, calibrationData.Extension_speed, true)
		time.Sleep(time.Duration(int64(calibrationData.Extension_limit)-ext_i.Milliseconds()) * time.Millisecond)

		// Drop down
		move(0, -calibrationData.Upwards_speed, 0, true)
		time.Sleep(up_i)

		// Release object
		move(0, 0, 0, false)

		// Move back up
		move(0, calibrationData.Upwards_speed, 0, false)
		time.Sleep(up_i)

		// Move to the center of the extension
		move(0, 0, -calibrationData.Extension_speed, false)
		time.Sleep(time.Duration(calibrationData.Extension_limit/2) * time.Millisecond)
		ext_i = time.Duration(calibrationData.Extension_limit/2) * time.Millisecond

		// Return to last known position
		move(calibrationData.Rotation_speed, 0, 0, true)
		time.Sleep(time.Duration(rot_i.Milliseconds()/int64(calibrationData.Rotation_revolution)) * time.Millisecond)

		return
	}

	if Cx != -1 && Cy != -1 {
		t := time.Now()
		// Object can be seen, but not grabbed
		rot := 0
		ext := 0
		// Check what we need move
		if Cx > int(calibrationData.Max_deviation) && float64(Cx) != img_Cx {
			rot = int(-(float64(Cx) - img_Cx) / math.Abs(float64(Cx)-img_Cx))
		}
		if Cy > int(calibrationData.Max_deviation) && float64(Cy) != img_Cx {
			ext = int((float64(Cy) - img_Cy) / math.Abs(float64(Cy)-img_Cy))
		}

		// Move
		move(rot*calibrationData.Rotation_speed, 0, ext*calibrationData.Extension_speed, false)
		rot_i += time.Since(t) * time.Duration(rot)
		ext_i += time.Since(t) * time.Duration(ext)
		return
	}

	// If nothing, move forward
	t := time.Now()
	move(calibrationData.Rotation_speed, 0, 0, false)
	rot_i += time.Since(t)
}

func manulaRoam() {
	com := globalCollectedData[6].content
	if com == "" {
		return
	}
	log.Printf("Received command: %s\n", fmt.Sprint(globalCollectedData[6]))
	globalCollectedData[6].content = ""

	switch com {
	case "F":
		move(0, 0, calibrationData.Extension_speed, false)
		interval := time.Duration(calibrationData.Manual_interval) * time.Millisecond
		time.Sleep(interval)
	case "R":
		move(calibrationData.Rotation_speed, 0, 0, false)
		interval := time.Duration(calibrationData.Manual_interval) * time.Millisecond
		time.Sleep(interval)
	case "B":
		move(0, 0, -calibrationData.Extension_speed, false)
		interval := time.Duration(calibrationData.Manual_interval) * time.Millisecond
		time.Sleep(interval)
	case "L":
		move(-calibrationData.Rotation_speed, 0, 0, false)
		interval := time.Duration(calibrationData.Manual_interval) * time.Millisecond
		time.Sleep(interval)
	case "U":
		move(0, calibrationData.Upwards_speed, 0, false)
		interval := time.Duration(calibrationData.Manual_interval) * time.Millisecond
		time.Sleep(interval)
	case "D":
		move(0, -calibrationData.Upwards_speed, 0, false)
		interval := time.Duration(calibrationData.Manual_interval) * time.Millisecond
		time.Sleep(interval)
	}

	move(0, 0, 0, false)
}
