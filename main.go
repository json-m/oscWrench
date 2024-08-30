package main

import (
	"fmt"
	"github.com/crgimenes/go-osc"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
)

func init() {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

type TrackerData struct {
	ID       int
	Position [3]float32
	Rotation [3]float32
}

type TrackerManager struct {
	trackers  map[int]*TrackerData
	mu        sync.RWMutex
	updateCh  chan TrackerData
	forwardCh chan TrackerData
}

func NewTrackerManager() *TrackerManager {
	tm := &TrackerManager{
		trackers:  make(map[int]*TrackerData),
		updateCh:  make(chan TrackerData, 10000), // Buffered channel
		forwardCh: make(chan TrackerData, 10000), // Buffered channel
	}
	go tm.processUpdates()
	return tm
}

func (tm *TrackerManager) processUpdates() {
	for data := range tm.updateCh {
		tm.mu.Lock()
		if tracker, exists := tm.trackers[data.ID]; exists {
			if detectOrientationInversion(tracker.Rotation, data.Rotation) {
				data.Rotation = invertOrientation(data.Rotation)
			}
		}
		tm.trackers[data.ID] = &data
		tm.mu.Unlock()

		tm.forwardCh <- data
	}
}

func (tm *TrackerManager) UpdateTracker(data TrackerData) {
	tm.updateCh <- data
}

func (tm *TrackerManager) GetTrackerData(id int) (TrackerData, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	if tracker, exists := tm.trackers[id]; exists {
		return *tracker, true
	}
	return TrackerData{}, false
}

func detectOrientationInversion(old, new [3]float32) bool {
	threshold := float32(170.0) // degrees
	for i := 0; i < 3; i++ {
		if math.Abs(float64(old[i]-new[i])) > float64(threshold) {
			return true
		}
	}
	return false
}

func invertOrientation(orientation [3]float32) [3]float32 {
	inverted := [3]float32{}
	for i := 0; i < 3; i++ {
		inverted[i] = orientation[i] + 180
		if inverted[i] > 180 {
			inverted[i] -= 360
		}
	}
	return inverted
}

func parseMessage(msg *osc.Message) (TrackerData, bool) {
	parts := strings.Split(msg.Address, "/")
	if len(parts) < 4 || parts[1] != "tracking" || parts[2] != "trackers" {
		return TrackerData{}, false
	}

	id, err := strconv.Atoi(parts[3])
	if err != nil {
		return TrackerData{}, false
	}

	if len(msg.Arguments) != 3 {
		return TrackerData{}, false
	}

	values := [3]float32{}
	for i := 0; i < 3; i++ {
		if v, ok := msg.Arguments[i].(float32); ok {
			values[i] = v
		} else {
			return TrackerData{}, false
		}
	}

	data := TrackerData{ID: id}
	if strings.Contains(msg.Address, "position") {
		data.Position = values
	} else if strings.Contains(msg.Address, "rotation") {
		data.Rotation = values
	} else {
		return TrackerData{}, false
	}

	return data, true
}

func main() {
	// todo: change these to config file
	addr := "127.0.0.1:9009" // this applications OSC listener
	destAddr := "127.0.0.1"  // destination OSC server address
	trackerManager := NewTrackerManager()

	// Start the forwarder
	go forwardUpdatedData(destAddr, trackerManager.forwardCh)

	d := osc.NewStandardDispatcher()
	err := d.AddMsgHandler("*", func(msg *osc.Message) {
		if strings.Contains(msg.Address, "tracking") {
			data, ok := parseMessage(msg)
			if !ok {
				return
			}
			trackerManager.UpdateTracker(data)
		}

		// todo: additional handlers here
	})

	if err != nil {
		log.Println(err)
		return
	}

	server := &osc.Server{
		Addr:       addr,
		Dispatcher: d,
	}

	// todo: informative senders here

	log.Println("Starting listener on", addr)
	if err := server.ListenAndServe(); err != nil {
		log.Println(err)
		return
	}
}

func forwardUpdatedData(destAddr string, forwardCh <-chan TrackerData) {
	client := osc.NewClient(destAddr, 9010) // todo: change these to config file
	for data := range forwardCh {
		// Send position
		if data.Position != [3]float32{} {
			posMsg := osc.NewMessage(fmt.Sprintf("/tracking/trackers/%d/position", data.ID))
			for _, v := range data.Position {
				posMsg.Append(v)
			}
			err := client.Send(posMsg)
			if err != nil {
				log.Printf("Error sending position: %v\n", err)
			}
		}

		// Send rotation
		if data.Rotation != [3]float32{} {
			rotMsg := osc.NewMessage(fmt.Sprintf("/tracking/trackers/%d/rotation", data.ID))
			for _, v := range data.Rotation {
				rotMsg.Append(v)
			}
			err := client.Send(rotMsg)
			if err != nil {
				log.Printf("Error sending rotation: %v\n", err)
			}
		}
	}
}
