package user

import (
	"context"

	pb "yao.io/grpc-over-websocket/proto/user/v1"
)

// 假设我们有一个UserService服务
type UserServiceServer struct {
	pb.UnimplementedUserServiceServer
}

func (s *UserServiceServer) GetUser(ctx context.Context, req *pb.GetUserRequest) (*pb.GetUserResponse, error) {
	return &pb.GetUserResponse{
		User: &pb.User{
			Id:   req.Id,
			Name: "Test User",
			Age:  25,
		},
	}, nil
}

func (s *UserServiceServer) CreateUser(ctx context.Context, req *pb.CreateUserRequest) (*pb.CreateUserResponse, error) {
	return &pb.CreateUserResponse{
		Id:      "12345",
		Message: "User created successfully",
	}, nil
}
