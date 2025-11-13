package remdeshid

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

type HIDCommand struct {
	EventType    string `json:"t"`
	EventSubType string `json:"s"`
	Data         string `json:"d"`
	PosX         int    `json:"x"` //Only used for mouse events
	PosY         int    `json:"y"` //Only used for mouse events
}

// HIDWebSocketHandler is a handler for the HID WebSocket connection
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (c *Controller) HIDWebSocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Failed to upgrade to websocket:", err)
		return
	}
	defer conn.Close()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Println("Error reading message:", err)
			break
		}
		log.Printf("Received: %s", message)

		//Try parsing the message as a HIDCommand
		var hidCmd HIDCommand
		if err := json.Unmarshal(message, &hidCmd); err != nil {
			log.Println("Error parsing message:", err)
			continue
		}

		//Send the command to the HID controller
		bytes, err := ConvHIDCommandToBytes(hidCmd)
		if err != nil {
			errmsg := map[string]string{"error": err.Error()}
			if err := conn.WriteJSON(errmsg); err != nil {
				log.Println("Error writing message:", err)
			}
			continue
		}

		if bytes[0] == OPR_TYPE_MOUSE_SCROLL {
			currentTime := time.Now().UnixMilli()
			//Sending scroll too fast will cause the HID controller to glitch
			if currentTime-c.lastScrollTime < 20 {
				log.Println("Ignoring scroll event due to rate limiting")
				continue
			}
			c.lastScrollTime = currentTime
		}

		fmt.Println("Sending bytes:", bytes)

		//Write the bytes to the serial port
		if err := c.Send(bytes); err != nil {
			errmsg := map[string]string{"error": err.Error()}
			if err := conn.WriteJSON(errmsg); err != nil {
				log.Println("Error writing message:", err)
			}
			continue
		}

		if err := conn.WriteMessage(websocket.TextMessage, []byte("ok")); err != nil {
			log.Println("Error writing message:", err)
			continue
		}
	}
}
