package docker

import (
	"context"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"

	"github.com/pborman/uuid"
)

func TestLoad(t *testing.T) {
	d := loader{}
	ctx := context.Background()

	localPath := path.Join(os.TempDir(), uuid.New())

	// "marmotcogs/codetest:latest:::/advanced"
	// "http://registry-1.docker.io/v2/marmotcogs/codetest:latest:::/advanced"
	if err := d.Load(ctx, "marmotcogs/codetest:latest:::/advanced", localPath); err != nil {
		t.Fatalf("TestLoad: unexpected error: %s", err)
	}
	defer os.Remove(localPath)

	_, err := ioutil.ReadFile(localPath)
	if err != nil {
		panic(err)
	}

	cmd := exec.Command(localPath, "--cog_desc=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("TestLoad: unexpected error when running cog: %s", err)
	}

	if !strings.Contains(string(out), "johnsiilver") {
		t.Fatalf("TestLoad: got %s, expected it to contain 'johnsiilver'", string(out))
	}
}
