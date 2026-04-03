package main

import (
	"context"
	"log"
	"time"

	"github.com/centrifugal/centrifuge-go"
	"google.golang.org/grpc"

	"yao.io/grpc-over-websocket/clientside"
	pb "yao.io/grpc-over-websocket/proto/user/v1"
	"yao.io/grpc-over-websocket/server/user"
)

func main() {
	endpoint := "ws://127.0.0.1:8080/ws"
	c := centrifuge.NewProtobufClient(endpoint, centrifuge.Config{
		Token: "fake-token",
	})
	gw, err := clientside.NewGRPCGateway(c)
	if err != nil {
		log.Fatal(err)
	}
	pb.RegisterUserServiceServer(gw, &user.UserServiceServer{})

	if err := c.Connect(); err != nil {
		log.Fatal(err)
	}
	testConn(clientside.NewConn(c))
}

func testConn(conn grpc.ClientConnInterface) {
	for {
		userServiceClient := pb.NewUserServiceClient(conn)
		resp, err := userServiceClient.GetUser(context.Background(), &pb.GetUserRequest{Id: "user1"})
		log.Printf("resp: %v, err: %v", resp, err)
		time.Sleep(time.Second)
	}
}
