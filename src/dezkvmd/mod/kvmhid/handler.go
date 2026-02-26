package kvmhid

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
)

// upgrader is used to upgrade HTTP connections to WebSocket connections
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// HIDWebSocketHandler handles incoming WebSocket connections for HID commands
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

		//Try parsing the message as a HIDCommand
		var hidCmd HIDCommand
		if err := json.Unmarshal(message, &hidCmd); err != nil {
			log.Println("Error parsing message:", err)
			continue
		}

		bytes, err := c.ConstructAndSendCmd(&hidCmd)
		if err != nil {
			errmsg := map[string]string{"error": err.Error()}
			if err := conn.WriteJSON(errmsg); err != nil {
				// Check for broken pipe error to handle closed websocket
				if err != nil && strings.Contains(err.Error(), "broken pipe") {
					log.Println("WebSocket connection closed (broken pipe), cleaning up")
					break
				}
				log.Println("Error writing message:", err)
				continue
			}
			log.Println("Error sending command:", err)
			continue
		}

		prettyBytes := ""
		for _, b := range bytes {
			prettyBytes += fmt.Sprintf("0x%02X ", b)
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(prettyBytes)); err != nil {
			if strings.Contains(err.Error(), "broken pipe") {
				log.Println("WebSocket connection closed (broken pipe), cleaning up")
				break
			}
			log.Println("Error writing message:", err)
			continue
		}

	}
}
