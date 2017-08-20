package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/user"
	"path"
	"syscall"
	"testing"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	log "github.com/golang/glog"
	pb "github.com/johnsiilver/cog/proto/cog"
	tpb "github.com/johnsiilver/cog/proto/test"
	"github.com/kylelemons/godebug/pretty"
	"github.com/pborman/uuid"
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

	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range tests {
		if test.load {
			if err := cli.Load(test.cog, SUDO(u.Username), Flag("--logtostderr")); err != nil {
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

func TestReloadChanged(t *testing.T) {
	loader := localLoader{}
	cli, err := New()
	if err != nil {
		t.Fatal(err)
	}

	srcFP, err := filePath(cogPath("success"))
	if err != nil {
		t.Fatal(err)
	}
	loc := path.Join(os.TempDir(), uuid.New())
	if err = copyFile(loc, srcFP); err != nil {
		t.Fatal(err)
	}
	defer syscall.Unlink(loc)

	successVer, err := loader.version(srcFP)
	if err != nil {
		t.Fatal(err)
	}
	log.Infof("successVer: %v", successVer)

	locPath := "localFile://" + loc

	if err = cli.Load(locPath); err != nil {
		t.Fatal(err)
	}

	srcFP, err = filePath(cogPath("was_crashed"))
	if err != nil {
		t.Fatal(err)
	}

	crashedVer, err := loader.version(srcFP)
	if err != nil {
		t.Fatal(err)
	}
	log.Infof("crashedVer: %v", crashedVer)

	if err = copyFile(loc, srcFP); err != nil {
		t.Fatal(err)
	}

	if err = cli.ReloadChanged(); err != nil {
		t.Fatal(err)
	}

	currVer, err := loader.version(srcFP)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(currVer, cli.cogs[locPath].version) {
		t.Errorf("Test TestReloadChanged: version not correct after ReloadChanged():\ngot: %v\nwant: %v\n", cli.cogs[locPath].version, currVer)
	}
}

func copyFile(dst, src string) error {
	if _, err := os.Stat(dst); err == nil {
		if err := syscall.Unlink(dst); err != nil {
			return err
		}
	}
	s, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("could not open source file %q: %s", s.Name(), err)
	}
	defer s.Close()

	d, err := os.OpenFile(dst, os.O_CREATE+os.O_RDWR+os.O_EXCL, 0700)
	if err != nil {
		return fmt.Errorf("could not open destination file %q: %s", d.Name(), err)
	}

	defer d.Close()

	_, err = io.Copy(d, s)
	return err
}
