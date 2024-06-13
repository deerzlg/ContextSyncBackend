package main

import (
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许所有CORS请求
	},
}

type Client struct {
	conn *websocket.Conn
	send chan []byte
}

var (
	clients    = make(map[*Client]bool)
	broadcast  = make(chan Message)
	register   = make(chan *Client)
	unregister = make(chan *Client)
	mutex      sync.Mutex
)

type Message struct {
	Data []byte
	From *Client
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade failed:", err)
		return
	}
	defer ws.Close()

	client := &Client{conn: ws, send: make(chan []byte, 256)}
	register <- client

	defer func() { unregister <- client }()
	go handleMessages(client)

	for {
		_, message, err := ws.ReadMessage()
		if err != nil {
			log.Println("Read failed:", err)
			break
		}
		broadcast <- Message{Data: message, From: client}
	}
}

func handleMessages(client *Client) {
	for message := range client.send {
		if err := client.conn.WriteMessage(websocket.TextMessage, message); err != nil {
			log.Println("Write failed:", err)
			break
		}
	}
}

func broadcastToAll() {
	for {
		select {
		case message := <-broadcast:
			mutex.Lock()
			for client := range clients {
				// 广播给所有除来源以外的client
				if client != message.From {
					select {
					case client.send <- message.Data:
					default:
						log.Println("Send buffer full. Dropping message")
					}
				}
			}
			mutex.Unlock()
		case client := <-register:
			mutex.Lock()
			clients[client] = true
			mutex.Unlock()
		case client := <-unregister:
			mutex.Lock()
			if _, ok := clients[client]; ok {
				delete(clients, client)
				close(client.send)
			}
			mutex.Unlock()
		}
	}
}

func main() {
	http.HandleFunc("/ws", handleConnections)
	go broadcastToAll()

	log.Println("HTTP server started on :7000")
	if err := http.ListenAndServe(":7000", nil); err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
