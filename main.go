package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

var client *redis.Client

var Users map[string]*websocket.Conn
var sub *redis.PubSub

const channelName = "demo-chat"

func init() {
	Users = map[string]*websocket.Conn{}
}

func main() {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	_, err := client.Ping(context.Background()).Result()
	if err != nil {
		log.Fatal("Ping failedd", err)
	}
	go func() {
		sub = client.Subscribe(context.Background(), channelName)

		messages := sub.Channel()
		for message := range messages {
			from := strings.Split(message.Payload, ":")[0]

			for user, peer := range Users {
				if from != user {
					msg := "[" + from + "says: " + string(strings.Split(message.Payload, ":")[1])
					peer.WriteMessage(websocket.TextMessage, []byte(msg))
				}
			}
		}
	}()
	http.HandleFunc("/chat", chat)
	server := http.Server{Addr: ":8080", Handler: nil}

	go func() {
		fmt.Println("Chat server started...")

		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Fatal("failed to start server", err)
		}
	}()

	exit := make(chan os.Signal, 1)

	signal.Notify(exit, syscall.SIGTERM, syscall.SIGINT)
	<-exit

	fmt.Println("exit signalled")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	defer cancel()

	for _, conn := range Users {
		conn.Close()
	}

	sub.Unsubscribe(context.Background(), channelName)

	sub.Close()
	server.Shutdown(ctx)

	fmt.Println("Chat closed...")

}

var upgrader = websocket.Upgrader{}

func chat(w http.ResponseWriter, r *http.Request) {
	user := strings.TrimPrefix(r.URL.Path, "/chat/")

	upgrader.CheckOrigin = func(r *http.Request) bool {
		return true
	}

	c, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		log.Fatal("protocol upgrade error", err)
		return
	}
	Users[user] = c
	fmt.Println(user, "joined the chat")

	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			_, ok := err.(*websocket.CloseError)
			if ok {
				fmt.Println("connection closed by:", user)
				err := c.Close()
				if err != nil {
					fmt.Println("close connection error", err)
				}
				delete(Users, user)
				fmt.Println("connection and user session closed")
			}
			break
		}

		client.Publish(context.Background(), channelName, user+":"+string(message)).Err()

		if err != nil {
			fmt.Println("publish error", err)
		}
	}
}
