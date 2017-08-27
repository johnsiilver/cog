// Package loaders provides the interfaces and methods for defining Cog
// loaders which pull a Cog from a source and set it up for running.
package loaders

import (
	"context"
	"fmt"
	"regexp"
)

var validHeader = regexp.MustCompile(`^[a-zA-Z0-9-]{3,}://`)

var (
	loaderRegistry = map[string]Loader{}
)

// Loader provides access to a repository for use in Loading Cogs.
type Loader interface {
	// Load loads the Cog at cogPath to the localPath.
	Load(ctx context.Context, cogPath, localPath string) error

	// Version retrieves the version of the Cog located at cogPath.  The verion is
	// unique to the header type, so comparing the same plugin at two different
	// header locations is not valid.
	Version(ctx context.Context, cogPath string) ([]byte, error)
}

// Register a header with a loader.  While we could just do this statically,
// this makes sure we don't ship with a bad header.
func Register(header string, l Loader) {
	if !validHeader.MatchString(header) {
		panic(fmt.Sprintf("tried to register header %q which is not in the right format", header))
	}

	if _, ok := loaderRegistry[header]; ok {
		panic(fmt.Sprintf("tried to register header %q more than once", header))
	}

	loaderRegistry[header] = l
}

// Lookup finds a Loader for cogPath.
func Lookup(cogPath string) (Loader, error) {
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

// FilePath splits the cogPath and just returns the file path portion.
func FilePath(cogPath string) (string, error) {
	split := validHeader.Split(cogPath, -1)
	if len(split) != 2 {
		return "", fmt.Errorf("the cog path %q does not have a valid header", cogPath)
	}
	return split[1], nil
}
