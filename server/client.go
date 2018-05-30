package server

import (
	"time"

	"github.com/golang/glog"
	"github.com/gorilla/websocket"
)

const (
	writeTimeout   = 10 * time.Second
	pongTimeout    = 30 * time.Second
	pingPeriod     = (pongTimeout * 9) / 10
	maxMessageSize = 65535
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1 << 20,
	WriteBufferSize: 1 << 20,
}

type Client struct {
	hub  Hub             // hub to register this client
	conn *websocket.Conn // websocket connection
	send chan []byte     // channel of outbound messages
	addr string          // client address
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				// channel has been closed by the hub
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				glog.V(8).Info("channel has been closed by the dispatcher")
				return
			}

			c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			w, err := c.conn.NextWriter(websocket.BinaryMessage)
			if err != nil {
				glog.Warningf("get next writer failed: %v", err)
				return
			}

			w.Write(message)

			if err := w.Close(); err != nil {
				glog.Warningf("close writer failed: %v", err)
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				// client closed unexpected
				glog.Warningf("ping client failed: %v", err)
				return
			}
		}
	}
}

func (c *Client) readPump() {
	defer func() {
		c.hub.Unregister() <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongTimeout))
	c.conn.SetPongHandler(
		func(string) error {
			c.conn.SetReadDeadline(time.Now().Add(pongTimeout))
			return nil
		},
	)

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				glog.Warningf("connection closed unexpected: %v", err)
			} else {
				glog.Warningf("read data failed: %v", err)
			}
			break
		}
		c.hub.ProcessData(data)
	}
}
