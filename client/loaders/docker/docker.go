// Package docker provides the ability to load a Cog from a Docker image.
// This should be imported as a side effect.
// This package registers the docker:// header.
package docker

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/golang/glog"
	"github.com/johnsiilver/cog/client/loaders"
)

func init() {
	loaders.Register("docker://", &loader{})
}

type clientRec struct {
	last   time.Time
	client *client.Client
}

type loader struct {
}

// load loads the Cog at cogPath to the localPath.
func (d *loader) Load(ctx context.Context, cogPath, localPath string) error {
	if fi, _ := os.Stat(localPath); fi != nil {
		if err := syscall.Unlink(localPath); err != nil {
			return fmt.Errorf("problem unlinking the current version at %q: %s", localPath, err)
		}
	}

	hp := strings.Split(cogPath, ":::")
	if len(hp) != 2 {
		return fmt.Errorf("cogPath should be <registry path>:::<cog name>:<tag>, was %s", cogPath)
	}

	if len(strings.Split(hp[0], ":")) != 2 {
		return fmt.Errorf("cogPath's cog name must have a tag, was %s", hp[0])
	}

	return d.getFile(ctx, hp[0], fmt.Sprintf("%s-%s-%s", path.Join("/cogs/", hp[1]), runtime.GOOS, runtime.GOARCH), localPath)
}

func (d *loader) getFile(ctx context.Context, imgPath, filePath, localPath string) error {
	cli, err := client.NewEnvClient()
	if err != nil {
		return fmt.Errorf("problem getting client to docker: %s", err)
	}

	glog.Infof("image path: %s", imgPath)
	glog.Infof("file path: %s", filePath)
	out, err := cli.ImagePull(ctx, imgPath, types.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("problem pulling %q from image %q: %s", filePath, imgPath, err)
	}
	defer out.Close()

	info, err := cli.ContainerCreate(
		ctx,
		&container.Config{
			Image: imgPath,
		},
		nil,
		nil,
		"",
	)
	if err != nil {
		return fmt.Errorf("could not create a container from image %q: %s", imgPath, err)
	}

	tarStream, _, err := cli.CopyFromContainer(ctx, info.ID, filePath)
	if err != nil {
		return err
	}

	tr := tar.NewReader(tarStream)
	_, err = tr.Next()
	if err != nil {
		return err
	}

	f, err := os.OpenFile(localPath, os.O_CREATE+os.O_WRONLY, 0700)
	if err != nil {
		return fmt.Errorf("problem creating cog at %s: %s", localPath, err)
	}

	if _, err := io.Copy(f, tr); err != nil {
		return fmt.Errorf("problem writing cog to %s: %s", localPath, err)
	}
	defer f.Close()

	if err := cli.ContainerRemove(ctx, info.ID, types.ContainerRemoveOptions{}); err != nil {
		return err
	}
	return nil
}

func (d *loader) Version(ctx context.Context, cogPath string) ([]byte, error) {
	return nil, nil
}
