package main

import (
	"context"
	"log"
	"time"

	"github.com/centrifugal/centrifuge"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"

	pb "yao.io/grpc-over-websocket/proto/user/v1"
	"yao.io/grpc-over-websocket/server/user"
	"yao.io/grpc-over-websocket/serverside"
)

type fakeAuthenticator struct{}

func (fakeAuthenticator) Authenticate(ctx context.Context, token string, data []byte) (*centrifuge.Credentials, []byte, bool, error) {
	return &centrifuge.Credentials{UserID: "fake-user"}, nil, true, nil
}

func main() {
	gw, err := serverside.NewGRPCGateway(fakeAuthenticator{})
	if err != nil {
		log.Fatal(err)
	}
	pb.RegisterUserServiceServer(gw, &user.UserServiceServer{})

	if err = gw.StartServing(); err != nil {
		log.Fatal(err)
	}
	go testConn(gw.ConnPool())

	router := gin.Default()
	router.GET("/ws", gin.WrapH(gw.NewHTTPHandler()))

	if err := router.Run(":8080"); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
}

func testConn(conn grpc.ClientConnInterface) {
	ctx := serverside.WithUserID(context.Background(), "fake-user")

	for {
		client := pb.NewUserServiceClient(conn)
		resp, err := client.GetUser(ctx, &pb.GetUserRequest{})
		log.Printf("resp: %v, err: %v", resp, err)
		time.Sleep(time.Second)
	}
}
