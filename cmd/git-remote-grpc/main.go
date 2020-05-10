// Command git-remote-grpc is a git remote-helper to perform
// authenticated Git operations over gRPC.
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/encoredev/git-remote-grpc/gitpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

//go:generate protoc -I . --go_out=plugins=grpc,paths=source_relative:./ ./gitpb/gitpb.proto

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", os.Args[0], err)
		os.Exit(1)
	}
}

func run(args []string) error {
	stdin := bufio.NewReader(os.Stdin)
	stdout := os.Stdout

	// Read commands from stdin.
	for {
		cmd, err := stdin.ReadString('\n')
		if err != nil {
			return fmt.Errorf("unexpected error reading stdin: %v", err)
		}
		cmd = cmd[:len(cmd)-1] // skip trailing newline
		switch {
		case cmd == "capabilities":
			if _, err := stdout.Write([]byte("*connect\n\n")); err != nil {
				return err
			}
		case strings.HasPrefix(cmd, "connect "):
			service := cmd[len("connect "):]
			return connect(args, service, stdin, stdout)
		default:
			return fmt.Errorf("unsupported command: %s", cmd)
		}
	}
}

// connect implements the "connect" capability by copying data
// to and from the remote end over gRPC.
func connect(args []string, svc string, stdin io.Reader, stdout io.Writer) error {
	stream, err := grpcConnect(args, svc)
	if err != nil {
		return err
	}

	// Communicate to Git that the connection is established
	os.Stdout.Write([]byte("\n"))

	// writeErr captures the error from the write side.
	// nil indicates stdin was closed and everything was successful.
	// io.EOF indicates a failure to send the message on the stream.
	writeErr := make(chan error, 1)

	// Writer goroutine that reads from stdin and writes to the stream.
	// Sends on writeErr when done sending. nil indicates success and
	// a non-nil error indicates something went wrong.
	go func() {
		var buf [1024]byte
		for {
			n, readErr := stdin.Read(buf[:])
			if n > 0 {
				if err := stream.Send(&gitpb.Data{Data: buf[:n]}); err != nil {
					writeErr <- fmt.Errorf("write: %v", err)
					return
				}
			}

			if readErr != nil {
				if readErr == io.EOF {
					stream.CloseSend()
					writeErr <- nil
				} else {
					writeErr <- fmt.Errorf("reading stdin: %v", readErr)
				}
				return
			}
		}
	}()

	// Read from the stream and copy it to stdout.
	// If the reads complete successfully it waits
	// for the write end to complete before returning.
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			// No more data from the server.
			// Wait for the write end to complete.
			if err := <-writeErr; err != nil {
				return err
			}
			return nil
		} else if err != nil {
			return fmt.Errorf("read: %v", err)
		} else {
			if _, err := stdout.Write(msg.Data); err != nil {
				return fmt.Errorf("writing stdout: %v", err)
			}
		}
	}
}

// grpcConnect parses the remote address from the args
// and invokes the Connect endpoint.
func grpcConnect(args []string, svc string) (gitpb.Git_ConnectClient, error) {
	var (
		addr *url.URL
		err  error
	)
	switch {
	case len(args) >= 3:
		addr, err = url.Parse(args[2])
	case len(args) == 2:
		addr, err = url.Parse(args[1])
	default:
		err = fmt.Errorf("no address given")
	}
	if err != nil {
		return nil, fmt.Errorf("parsing remote address: %v", err)
	}

	conn, err := grpc.Dial(addr.Host, grpc.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("dial %s: %v", addr.Host, err)
	}
	gitClient := gitpb.NewGitClient(conn)

	// Identify the repository by the address path.
	repoPath := strings.TrimPrefix(addr.Path, "/")
	ctx := metadata.AppendToOutgoingContext(context.Background(),
		"service", svc,
		"repository", repoPath,
	)
	return gitClient.Connect(ctx)
}
