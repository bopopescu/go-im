package main

import (
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
	"sync"
	"time"
)

// http升级websocket协议的配置
var wsUpgrader = websocket.Upgrader{
	// 允许所有CORS跨域请求
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// 客户端读写消息
type wsMessage struct {
	messageType int
	data        []byte
}

// 客户端连接
type wsConnection struct {
	wsSocket *websocket.Conn // 底层websocket
	inChan   chan *wsMessage // 读队列
	outChan  chan *wsMessage // 写队列
	
	mutex     sync.Mutex // 避免重复关闭管道
	isClosed  bool
	closeChan chan byte // 关闭通知
}

func (wsConn *wsConnection) wsReadLoop() {
	for {
		// 读一个message
		msgType, data, err := wsConn.wsSocket.ReadMessage()
		if err != nil {
			goto error
		}
		fmt.Println(string(data))
		req := &wsMessage{
			msgType,
			data,
		}
		// 放入请求队列
		select {
		case wsConn.inChan <- req:
		case <-wsConn.closeChan:
			goto closed
		}
	}
error:
	wsConn.wsClose()
closed:
}

func (wsConn *wsConnection) wsWriteLoop() {
	for {
		select {
		// 取一个应答
		case msg := <-wsConn.outChan:
			// 写给websocket
			if err := wsConn.wsSocket.WriteMessage(msg.messageType, msg.data); err != nil {
				goto error
			}
		case <-wsConn.closeChan:
			goto closed
		}
	}
error:
	wsConn.wsClose()
closed:
}

func (wsConn *wsConnection) procLoop() {
	// 启动一个gouroutine发送心跳
	go func() {
		for {
			time.Sleep(2 * time.Second)
			if err := wsConn.wsWrite(websocket.TextMessage, []byte("heartbeat from server")); err != nil {
				fmt.Println("heartbeat fail")
				wsConn.wsClose()
				break
			}
		}
	}()
	
}

func wsHandler(resp http.ResponseWriter, req *http.Request) {
	// 应答客户端告知升级连接为websocket
	wsSocket, err := wsUpgrader.Upgrade(resp, req, nil)
	
	if err != nil {
		return
	}
	
	fmt.Println("connection success")
	
	wsConn := &wsConnection{
		wsSocket:  wsSocket,
		inChan:    make(chan *wsMessage, 1000),
		outChan:   make(chan *wsMessage, 1000),
		closeChan: make(chan byte),
		isClosed:  false,
	}
	
	// 处理器
	go wsConn.procLoop()
	// 读协程
	go wsConn.wsReadLoop()
	// 写协程
	go wsConn.wsWriteLoop()
}

func (wsConn *wsConnection) wsWrite(messageType int, data []byte) error {
	select {
	case wsConn.outChan <- &wsMessage{messageType, data,}:
	case <-wsConn.closeChan:
		return errors.New("websocket closed")
	}
	return nil
}

func (wsConn *wsConnection) wsRead() (*wsMessage, error) {
	select {
	case msg := <-wsConn.inChan:
		return msg, nil
	case <-wsConn.closeChan:
	}
	return nil, errors.New("websocket closed")
}

func (wsConn *wsConnection) wsClose() {
	wsConn.wsSocket.Close()
	
	wsConn.mutex.Lock()
	defer wsConn.mutex.Unlock()
	if !wsConn.isClosed {
		wsConn.isClosed = true
		close(wsConn.closeChan)
	}
}

func main() {
	http.HandleFunc("/ws", wsHandler)
	http.ListenAndServe("0.0.0.0:7777", nil)
}
