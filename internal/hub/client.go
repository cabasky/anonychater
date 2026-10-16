package hub

import (
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"

	pb "anonychater/proto"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 75 * time.Second
	pingPeriod     = 25 * time.Second
	maxMessageSize = 2 << 20
	sendBuffer     = 256
)

type Client struct {
	hub  *Hub
	conn *websocket.Conn
	Send chan []byte

	mu         sync.Mutex
	clientID   string
	userID     string
	senderName string
}

func NewClient(h *Hub, conn *websocket.Conn, clientID, userID, name string) *Client {
	return &Client{
		hub:        h,
		conn:       conn,
		Send:       make(chan []byte, sendBuffer),
		clientID:   clientID,
		userID:     userID,
		senderName: name,
	}
}

func (c *Client) ClientID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.clientID
}

func (c *Client) UserID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.userID
}

func (c *Client) SenderName() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.senderName
}

func (c *Client) SetIdentity(id, name string) {
	c.mu.Lock()
	c.userID = id
	c.senderName = name
	c.mu.Unlock()
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.Send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
		c.hub.OnLeave(c)
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		var msg pb.ChatMessage
		if err := proto.Unmarshal(data, &msg); err != nil {
			log.Printf("unmarshal: %v", err)
			continue
		}
		c.hub.HandleMessage(c, &msg)
	}
}