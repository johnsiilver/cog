package client

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"syscall"
)

var validHeader = regexp.MustCompile(`^[a-zA-Z0-9-]{3,}://`)

var (
	loaderRegistry = map[string]loader{}
)

func init() {
	register("localFile://", localLoader{})
}

type loader interface {
	// load loads the Cog at cogPath to the localPath.
	load(cogPath, localPath string) error

	// version retrieves the version of the Cog located at cogPath.  The verion is
	// unique to the header type, so comparing the same plugin at two different
	// header locations is not valid.
	version(cogPath string) ([]byte, error)
}

// register a heder with a loader.  While we could just do this statically,
// this makes sure we don't ship with a bad header.
func register(header string, l loader) {
	if !validHeader.MatchString(header) {
		panic(fmt.Sprintf("tried to register header %q which is not in the right format", header))
	}

	if _, ok := loaderRegistry[header]; ok {
		panic(fmt.Sprintf("tried to register header %q more than once", header))
	}

	loaderRegistry[header] = l
}

// lookupLoader finds a loader for cogPath.
func lookupLoader(cogPath string) (loader, error) {
	header := validHeader.FindAllString(cogPath, 1)
	if len(header) != 1 {
		return nil, fmt.Errorf("the cog path %q does not have a valid header: %#v", cogPath, header)
	}

	l := loaderRegistry[header[0]]
	if l == nil {
		return nil, fmt.Errorf("could not find a registered loader for header %q", header[0])
	}
	return l, nil
}

// filePath splits the cogPath and just returns the file path portion.
func filePath(cogPath string) (string, error) {
	split := validHeader.Split(cogPath, -1)
	if len(split) != 2 {
		return "", fmt.Errorf("the cog path %q does not have a valid header", cogPath)
	}
	return split[1], nil
}

// localLoader copies a file from pluginPath to localPath.  If the localPath
// already exists, it should simply stat the file and make sure it has perms
// set to 0700.
type localLoader struct{}

// load implements loader.load().
func (l localLoader) load(pluginPath, localPath string) error {
	_, err := os.Stat(pluginPath)
	if err != nil {
		return fmt.Errorf("source file %q is unaccessible: %s", pluginPath, err)
	}

	srcVer, err := l.version(pluginPath)
	if err != nil {
		return err
	}

	fi, _ := os.Stat(localPath)
	if fi != nil {
		dstVer, err := l.version(localPath)
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

// version implements loader.version().
func (localLoader) version(cogPath string) ([]byte, error) {
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
