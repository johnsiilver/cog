package client

import (
	"bytes"
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/kylelemons/godebug/pretty"
)

func TestLocalLoader(t *testing.T) {
	const (
		out     = "hello world"
		srcName = "src"
		dstName = "dst"
	)

	src := path.Join(os.TempDir(), srcName)
	dst := path.Join(os.TempDir(), dstName)

	if err := ioutil.WriteFile(src, []byte(out), 0700); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(src)

	if err := localLoader(src, dst); err != nil {
		t.Fatalf("TestLocalLoader: unexpected error: %s", err)
	}
	defer os.Remove(dst)

	b, err := ioutil.ReadFile(dst)
	if err != nil {
		t.Fatalf("TestLocalLoader: could not read output file: %s", err)
	}

	if !bytes.Equal([]byte(out), b) {
		t.Fatalf("TestLocalLoader: -want/+got:\n%s", pretty.Sprint(out, string(b)))
	}
}
