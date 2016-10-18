package client

import (
	"context"
	"os"
	"path"
	"testing"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	pb "github.com/johnsiilver/cog/proto/cog"
	tpb "github.com/johnsiilver/cog/proto/test"
	"github.com/kylelemons/godebug/pretty"
)

var jsonMarshaller = &jsonpb.Marshaler{Indent: "\t"}

func cogPath(cog string) string {
	return "localFile://" + path.Join(os.Getenv("GOPATH"), "src/github.com/johnsiilver/cog/client/test_cogs/", cog, cog)
}

func TestExecute(t *testing.T) {
	tests := []struct {
		desc string
		cog  string
		load bool
		err  bool
	}{
		{
			desc: "Cog does not exist",
			cog:  cogPath("notExist"),
			err:  true,
		},
		{
			desc: "Cog is not loaded",
			cog:  cogPath("success"),
			err:  true,
		},
		{
			desc: "Cog crashes during execution",
			cog:  cogPath("was_crashed"),
			load: true,
			err:  true,
		},
		{
			desc: "Success",
			cog:  cogPath("success"),
			load: true,
		},
	}

	cli, err := New()
	if err != nil {
		t.Fatal(err)
	}

	expectedOut, err := jsonMarshaller.MarshalToString(
		&tpb.Response{
			Answer: "worked",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range tests {
		if test.load {
			if err := cli.Load(test.cog); err != nil {
				t.Fatalf("Test %q: %s", test.desc, err)
			}
		}

		status, out, err := cli.Execute(context.Background(), test.cog, "", []byte("{}"), pb.ArgsType_JSON, nil)
		switch {
		case test.err && err == nil:
			t.Errorf("Test %q: got err == nil, want err != nil", test.desc)
			continue
		case !test.err && err != nil:
			t.Errorf("Test %q: got err == %s, want err == nil", test.desc, err)
			continue
		case err != nil:
			continue
		}

		if status != pb.Status_SUCCESS {
			t.Errorf("Test %q: got status %q, want %q", test.desc, status, pb.Status_SUCCESS)
			continue
		}

		if diff := pretty.Compare(expectedOut, string(out)); diff != "" {
			t.Errorf("Test %q: out -want/+got:\n%s", test.desc, diff)
		}
	}
}

func TestUnload(t *testing.T) {
	cli, err := New()
	if err != nil {
		t.Fatal(err)
	}

	if err := cli.Load(cogPath("success")); err != nil {
		t.Fatal(err)
	}

	cli.cogsMu.Lock()
	co := cli.cogs[cogPath("success")]
	cli.cogsMu.Unlock()

	if co.cmd.ProcessState != nil {
		t.Fatalf("cmd should not have exited yet")
	}

	if err := cli.Unload(cogPath("success")); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {

		if time.Now().After(deadline) {
			t.Fatalf("plugin never unloaded")
		} else if co.cmd.ProcessState == nil {
			time.Sleep(1 * time.Second)
		} else {
			break
		}
	}
}
