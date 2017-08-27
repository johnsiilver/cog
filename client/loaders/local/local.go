// Package local provides the ability to load a Cog from the local file system.
// This should be imported as a side effect.
// This package registers the localFile:// header.
package local

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"syscall"

	"github.com/johnsiilver/cog/client/loaders"
)

func init() {
	loaders.Register("localFile://", Loader{})
}

// Loader copies a file from pluginPath to localPath.  If the localPath
// already exists, it should simply stat the file and make sure it has perms
// set to 0700.
type Loader struct{}

// Load implements Loader.load().
func (l Loader) Load(ctx context.Context, pluginPath, localPath string) error {
	_, err := os.Stat(pluginPath)
	if err != nil {
		return fmt.Errorf("source file %q is unaccessible: %s", pluginPath, err)
	}

	srcVer, err := l.Version(ctx, pluginPath)
	if err != nil {
		return err
	}

	fi, _ := os.Stat(localPath)
	if fi != nil {
		dstVer, err := l.Version(ctx, localPath)
		if err != nil {
			return err
		}
		// If the versions are equal, just chmod the one there and do nothing.
		// Otherwise unlink it so we can copy the new version.
		if bytes.Equal(srcVer, dstVer) {
			if err = os.Chmod(localPath, 0700); err != nil {
				return err
			}
			return nil
		}
		if err = syscall.Unlink(localPath); err != nil {
			return err
		}
	}

	src, err := os.Open(pluginPath)
	if err != nil {
		return fmt.Errorf("could not open source file %q: %s", pluginPath, err)
	}
	defer src.Close()

	dst, err := os.OpenFile(localPath, os.O_CREATE+os.O_RDWR+os.O_EXCL, 0700)
	if err != nil {
		return fmt.Errorf("could not open destination file %q: %s", localPath, err)
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// Version implements Loader.Version().
func (Loader) Version(ctx context.Context, cogPath string) ([]byte, error) {
	stat, err := os.Stat(cogPath)
	if err != nil {
		return nil, fmt.Errorf("could not stat file %q: %s", cogPath, err)
	}

	if stat.IsDir() {
		return nil, fmt.Errorf("cogPath cannot be a directory")
	}

	f, err := os.Open(cogPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	hasher := sha256.New()

	if _, err := io.Copy(hasher, f); err != nil {
		log.Fatal(err)
	}

	return hasher.Sum(nil), nil
}
