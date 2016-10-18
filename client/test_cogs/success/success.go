package main

import (
	"flag"
	"fmt"

	"github.com/golang/protobuf/proto"
	"github.com/johnsiilver/cog"
	pb "github.com/johnsiilver/cog/proto/cog"
	tpb "github.com/johnsiilver/cog/proto/test"
	"golang.org/x/net/context"
)

type testCog struct {
}

func (t *testCog) Execute(ctx context.Context, args proto.Message, realUser, endpoint, id string) (cog.Out, error) {
	return cog.Out{Status: cog.Success, Output: &tpb.Response{Answer: "worked"}}, nil
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

func main() {
	flag.Parse()
	if err := cog.Start(&testCog{}); err != nil {
		panic(err)
	}
	select {}
}
