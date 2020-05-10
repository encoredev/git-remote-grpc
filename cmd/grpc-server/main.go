package main

import (
	"bytes"
	"flag"
	"log"
	"net"
	"os/exec"

	"github.com/encoredev/git-remote-grpc/gitpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func main() {
	addr := flag.String("addr", "localhost:8080", "gRPC listen address")
	flag.Parse()
	log.Println("listening for grpc on", *addr)
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	srv := grpc.NewServer()
	gitpb.RegisterGitServer(srv, &Server{})
	log.Fatalln(srv.Serve(ln))
}

const (
	pushSvc  = "git-receive-pack"
	fetchSvc = "git-upload-pack"
)

type Server struct {
}

// Assert that Server implements gitpb.GitServer.
var _ gitpb.GitServer = (*Server)(nil)

func (s *Server) Connect(stream gitpb.Git_ConnectServer) error {
	// Parse the repository path and service to invoke from the gRPC header.
	// Note that the svc is invoked directly as an executable, so parseHeader
	// must validate these very carefully!
	svc, repoPath, err := s.parseHeader(stream)
	if err != nil {
		return err
	}

	var stderr bytes.Buffer
	cmd := exec.Command(svc, repoPath)
	cmd.Stdin = &streamReader{stream: stream}
	cmd.Stdout = &streamWriter{stream: stream}
	cmd.Stderr = &stderr

	if err := cmd.Run(); err == nil {
		return nil
	}
	return status.Errorf(codes.Internal, "%s failed: %s", svc, stderr.Bytes())
}

// parseHeader parses the gRPC header and validates the service and repository paths.
func (s *Server) parseHeader(stream gitpb.Git_ConnectServer) (service, repoPath string, err error) {
	ctx := stream.Context()
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", "", status.Error(codes.InvalidArgument, "missing stream metadata")
	}

	repo := md.Get("repository")
	svc := md.Get("service")
	if len(repo) != 1 || len(svc) != 1 {
		return "", "", status.Errorf(codes.InvalidArgument, "invalid repository (%v) or service (%v)", repo, svc)
	}

	// DANGER: Check the service name against the whitelist to guard against remote execution.
	if svc[0] != pushSvc && svc[0] != fetchSvc {
		return "", "", status.Errorf(codes.InvalidArgument, "bad service: %s", svc[0])
	}

	// TODO: Change this to your own validation logic to make sure the repository is one
	// you want to expose.
	//if true {
	//	return "", "", status.Errorf(codes.InvalidArgument, "unknown repository: %s", repo[0])
	//}

	return svc[0], repo[0], nil
}

// streamReader implements io.Reader by reading from stream.
type streamReader struct {
	stream gitpb.Git_ConnectServer
	buf    []byte
}

func (sr *streamReader) Read(p []byte) (int, error) {
	// If we have remaining data from the previous message we received
	// from the stream, simply return that.
	if len(sr.buf) > 0 {
		n := copy(p, sr.buf)
		sr.buf = sr.buf[n:]
		return n, nil
	}

	// No more buffered data, wait for a new message from the stream.
	msg, err := sr.stream.Recv()
	if err != nil {
		return 0, err
	}
	// Read as much data as possible directly to the waiting caller.
	// Anything remaining beyond that gets buffered until the next Read call.
	n := copy(p, msg.Data)
	sr.buf = msg.Data[n:]
	return n, nil
}

// streamWriter implements io.Writer by sending the data on stream.
type streamWriter struct {
	stream gitpb.Git_ConnectServer
}

func (sw *streamWriter) Write(p []byte) (int, error) {
	err := sw.stream.Send(&gitpb.Data{Data: p})
	if err != nil {
		return 0, err
	}
	return len(p), nil
}
