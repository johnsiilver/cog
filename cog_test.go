package cog

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"path"
	"testing"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	pb "github.com/johnsiilver/cog/proto/cog"
	tpb "github.com/johnsiilver/cog/proto/test"
	"github.com/pborman/uuid"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

type testCog struct {
}

func (t *testCog) Execute(ctx context.Context, args proto.Message, realUser, endpoint, id string) (Out, error) {
	tArgs := args.(*tpb.Args)

	return Out{Status: Success, Output: tArgs}, nil
}

func (t *testCog) Describe() *pb.Description {
	return &pb.Description{
		Owner:       "johnsiilver",
		Description: "Plugin desc",
		Tags:        []string{"yo", "mama"},
	}
}

func (t *testCog) Validate(p proto.Message) error {
	if _, ok := p.(*tpb.Args); !ok {
		return fmt.Errorf("wrong type of args")
	}
	return nil
}

func (t *testCog) ArgsProto() proto.Message {
	return &tpb.Args{}
}

func TestCog(t *testing.T) {
	c := &testCog{}

	socketAddr := path.Join(os.TempDir(), "@"+uuid.New())

	l, err := net.Listen("unix", socketAddr)
	if err != nil {
		t.Fatal("listen error:", err)
	}

	if err = os.Chmod(l.Addr().String(), 0700); err != nil {
		t.Fatal(err)
	}

	done := make(chan []byte)
	var tok []byte
	go func() {
		defer close(done)

		fd, err := l.Accept()
		if err != nil {
			t.Fatal("accept error:", err)
		}

		log.Infof("reading size")
		size := int64(0)
		if err = binary.Read(fd, binary.BigEndian, &size); err != nil {
			t.Fatal(err)
		}

		log.Infof("reading address")
		addr := make([]byte, size)
		if _, err = fd.Read(addr); err != nil {
			t.Fatal(err)
		}

		log.Infof("reading token size")
		if err = binary.Read(fd, binary.BigEndian, &size); err != nil {
			t.Fatal(err)
		}

		log.Infof("reading token")
		tok = make([]byte, size)
		if _, err := fd.Read(tok); err != nil {
			t.Fatal(err)
		}

		log.Infof("send ack")
		if _, err := fd.Write([]byte("ack")); err != nil {
			t.Fatal(err)
		}

		done <- addr
	}()

	go func() {
		if err := Start(c, Sock(socketAddr)); err != nil {
			t.Fatal(err)
		}
	}()

	var addr []byte
	select {
	case addr = <-done:
		log.Infof(string(addr))
	case <-time.After(20 * time.Second):
		t.Fatal("never saw addr")
	}

	conn, err := grpc.Dial(string(addr), grpc.WithInsecure())
	if err != nil {
		t.Fatal(err)
	}
	client := pb.NewCogServiceClient(conn)

	args, err := (&jsonpb.Marshaler{}).MarshalToString(&tpb.Args{Say: "hello"})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := client.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	err = stream.Send(
		&pb.ExecuteRequest{
			Args: &pb.Args{
				ArgsType: pb.ArgsType_JSON,
				Args:     []byte(args),
			},
			Server: &pb.Server{},
			Token:  tok,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}

	out := &tpb.Args{}
	if err := jsonpb.UnmarshalString(string(resp.Out.Output), out); err != nil {
		t.Fatal(err)
	}

	if out.Say != "hello" {
		t.Fatalf("got %q, want %q", out.Say, "hello")
	}
}
