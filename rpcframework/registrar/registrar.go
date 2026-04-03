package registrar

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/status"
)

var logger = grpclog.Component("registrar")

// GRPCServiceRegistrar 实现grpc.ServiceRegistrar接口
type GRPCServiceRegistrar struct {
	mu       sync.Mutex              // guards following
	services map[string]*serviceInfo // service name -> service info
}

// serviceInfo wraps information about a service. It is very similar to
// ServiceDesc and is constructed from it for internal purposes.
type serviceInfo struct {
	// Contains the implementation for the methods in this service.
	serviceImpl any
	methods     map[string]*grpc.MethodDesc
	mdata       any
}

// NewGRPCServiceRegistrar 创建一个新的动态服务注册器
func NewGRPCServiceRegistrar() *GRPCServiceRegistrar {
	return &GRPCServiceRegistrar{
		services: make(map[string]*serviceInfo),
	}
}

// 实现grpc.ServiceRegistrar接口
func (s *GRPCServiceRegistrar) RegisterService(sd *grpc.ServiceDesc, ss interface{}) {
	if ss != nil {
		ht := reflect.TypeOf(sd.HandlerType).Elem()
		st := reflect.TypeOf(ss)
		if !st.Implements(ht) {
			logger.Fatalf("grpc: Server.RegisterService found the handler of type %v that does not satisfy %v", st, ht)
		}
	}
	s.register(sd, ss)
}

func (s *GRPCServiceRegistrar) register(sd *grpc.ServiceDesc, ss interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.services[sd.ServiceName]; ok {
		logger.Fatalf("grpc: Server.RegisterService found duplicate service registration for %q", sd.ServiceName)
	}
	info := &serviceInfo{
		serviceImpl: ss,
		methods:     make(map[string]*grpc.MethodDesc),
		mdata:       sd.Metadata,
	}
	for i := range sd.Methods {
		d := &sd.Methods[i]
		info.methods[d.MethodName] = d
	}
	s.services[sd.ServiceName] = info
}

// CallGRPC 根据完整的方法名调用对应的gRPC方法
func (s *GRPCServiceRegistrar) CallGRPC(ctx context.Context, sm string, dec func(any) error) (any, error) {
	if len(sm) == 0 || sm[0] != '/' {
		return nil, status.Error(codes.Unimplemented, fmt.Sprintf("got invalid method %s", sm))
	}

	sm = sm[1:]
	pos := strings.LastIndex(sm, "/")
	if pos == -1 {
		return nil, status.Error(codes.Unimplemented, fmt.Sprintf("got invalid method %s", sm))
	}
	service := sm[:pos]
	method := sm[pos+1:]

	srv, knownService := s.services[service]
	if knownService {
		if md, ok := srv.methods[method]; ok {
			return s.processUnaryRPC(ctx, srv, md, dec)
		}
	}
	return nil, status.Error(codes.Unimplemented, fmt.Sprintf("method %s not implemented", sm))
}

func (s *GRPCServiceRegistrar) processUnaryRPC(ctx context.Context, info *serviceInfo, md *grpc.MethodDesc, df func(any) error) (any, error) {
	reply, appErr := md.Handler(info.serviceImpl, ctx, df, nil)
	if appErr != nil {
		appStatus, ok := status.FromError(appErr)
		if !ok {
			// Convert non-status application error to a status error with code
			// Unknown, but handle context errors specifically.
			appStatus = status.FromContextError(appErr)
			appErr = appStatus.Err()
		}
		return nil, appErr
	}
	return reply, appErr
}

// GetRegisteredServices 获取所有已注册的服务名称
func (d *GRPCServiceRegistrar) GetRegisteredServices() []string {
	services := make([]string, 0, len(d.services))
	for name := range d.services {
		services = append(services, name)
	}
	return services
}

// GetServiceMethods 获取指定服务的所有方法
func (d *GRPCServiceRegistrar) GetServiceMethods(serviceName string) ([]string, error) {
	service, ok := d.services[serviceName]
	if !ok {
		return nil, fmt.Errorf("service not found: %s", serviceName)
	}

	methods := make([]string, 0, len(service.methods))
	for name := range service.methods {
		methods = append(methods, name)
	}
	return methods, nil
}
