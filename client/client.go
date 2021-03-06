// Package client provides a client for loading/unloading/executing against a
// Cog binary.
package client

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"time"

	log "github.com/golang/glog"
	"github.com/johnsiilver/cog/client/loaders"
	pb "github.com/johnsiilver/cog/proto/cog"
	"github.com/pborman/uuid"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

// CogCrashError indicates the error is because the Cog crashed.
type CogCrashError string

func (c CogCrashError) Error() string {
	return string(c)
}

// loadOptions holds options that are used to load a Cog binary.
type loadOptions struct {
	sudo  string
	flags []string
}

// LoadOption provides options for loading a Cog.
type LoadOption func(l *loadOptions)

// SUDO tells the loader to attempt to load the plugin as user "u".
func SUDO(u string) LoadOption {
	return func(l *loadOptions) {
		l.sudo = u
	}
}

// Flag passes a flag to the Cog during loading.
func Flag(f string) LoadOption {
	return func(l *loadOptions) {
		l.flags = append(l.flags, f)
	}
}

type cogInfo struct {
	cogPath  string
	listener net.Listener
	cmd      *exec.Cmd
	client   pb.CogServiceClient
	conn     *grpc.ClientConn
	cancel   context.CancelFunc
	wg       *sync.WaitGroup
	token    []byte
	version  []byte
}

// Client is used to access Cog plugins.
type Client struct {
	loadTimeout time.Duration
	cogsMu      sync.Mutex
	cogs        map[string]cogInfo

	loadingMu    sync.Mutex
	loadingLocks map[string]*sync.Mutex
}

// New is the constructor for Client.
func New() (*Client, error) {
	return &Client{
		loadTimeout:  30 * time.Second,
		cogs:         make(map[string]cogInfo, 10),
		loadingLocks: make(map[string]*sync.Mutex, 10),
	}, nil
}

// Loaded returns if cogPath is loaded.
func (c *Client) Loaded(cogPath string) bool {
	c.loadingMu.Lock()
	defer c.loadingMu.Unlock()
	_, ok := c.cogs[cogPath]
	return ok
}

// ReloadChanged reloads any plugins from source that have changed.
func (c *Client) ReloadChanged(ctx context.Context) error {
	c.cogsMu.Lock()
	var g errgroup.Group
	for _, cogi := range c.cogs {
		cogi := cogi
		g.Go(func() error {
			load, err := loaders.Lookup(cogi.cogPath)
			if err != nil {
				return err
			}
			fp, err := loaders.FilePath(cogi.cogPath)
			if err != nil {
				return nil
			}
			v, err := load.Version(ctx, fp)
			if err != nil {
				return err
			}
			if !bytes.Equal(cogi.version, v) {
				log.Infof("cog %q version changed, loading new version", cogi.cogPath)
				if err := c.Unload(cogi.cogPath); err != nil {
					return err
				}
				return c.Load(ctx, cogi.cogPath)
			}
			return nil
		})
	}
	c.cogsMu.Unlock()

	if err := g.Wait(); err != nil {
		return err
	}

	return nil
}

// Load loads a cog at "cogPath".
func (c *Client) Load(ctx context.Context, cogPath string, options ...LoadOption) error {
	// Prevent any other loads while we get a lock for loading this specific plugin.
	c.loadingMu.Lock()

	opts := &loadOptions{}
	for _, mod := range options {
		mod(opts)
	}

	// Check to see if anyone loaded this plugin while we waited for the loadingMu.Lock().
	if _, ok := c.cogExist(cogPath); ok {
		c.loadingMu.Unlock()
		return nil
	}

	// Now get a cogPath specific lock.
	cogLock, ok := c.loadingLocks[cogPath]
	if !ok {
		cogLock = &sync.Mutex{}
		c.loadingLocks[cogPath] = cogLock
	}

	cogLock.Lock()
	defer func() {
		delete(c.loadingLocks, cogPath)
		cogLock.Unlock()
	}()

	// Check one more time to make sure no one sneaked a load in on the cogLock.
	if _, ok := c.cogExist(cogPath); ok {
		c.loadingMu.Unlock()
		return nil
	}
	c.loadingMu.Unlock()

	load, err := loaders.Lookup(cogPath)
	if err != nil {
		return err
	}

	fp, err := loaders.FilePath(cogPath)
	if err != nil {
		return nil
	}

	ver, err := load.Version(ctx, fp)
	if err != nil {
		return err
	}

	localPath := path.Join(os.TempDir(), path.Base(fp))
	if err = load.Load(ctx, fp, localPath); err != nil {
		return fmt.Errorf("problem copying Cog to local path: %s", err)
	}

	socketAddr, ch := c.socketSetup()

	ctx, cancel := context.WithCancel(context.Background())

	cmdStr := fmt.Sprintf("%s %s %s", localPath, socketAddr, strings.Join(opts.flags, " "))
	if opts.sudo != "" {
		cmdStr = fmt.Sprintf("sudo -u %s %s", opts.sudo, cmdStr)
	}

	log.Infof("launching binary with command %q", cmdStr)
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", cmdStr)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("problem starting Cog binary %q: %s", cogPath, err)
	}

	var sInfo socketInfo
	select {
	case sInfo = <-ch:
	case <-time.After(30 * time.Second):
		cancel()
		return fmt.Errorf("Cog did not acknowledge startup after 30 seconds")
	}

	if sInfo.err != nil {
		cancel()
		return fmt.Errorf("problem starting Cog: %s", err)
	}

	log.Infof("dialing %q", sInfo.addr)
	conn, err := grpc.Dial(sInfo.addr, grpc.WithInsecure())
	if err != nil {
		cancel()
		return fmt.Errorf("problem dialing the grpc of the Cog: %s", err)
	}

	ci := cogInfo{
		cogPath:  cogPath,
		listener: sInfo.listener,
		cmd:      cmd,
		client:   pb.NewCogServiceClient(conn),
		conn:     conn,
		cancel:   cancel,
		wg:       &sync.WaitGroup{},
		token:    sInfo.token,
		version:  ver,
	}
	go func() {
		cmd.Wait()
		ci.conn.Close()
	}()

	c.cogsMu.Lock()
	c.cogs[cogPath] = ci
	c.cogsMu.Unlock()

	return nil
}

// Unload unloads a Cog.  Existing calls will finish.
func (c *Client) Unload(cogPath string) error {
	c.loadingMu.Lock()
	defer c.loadingMu.Unlock()
	c.cogsMu.Lock()
	defer c.cogsMu.Unlock()

	co, ok := c.cogs[cogPath]
	if !ok {
		return nil
	}

	delete(c.cogs, cogPath)
	go func() {
		co.wg.Wait()
		co.listener.Close()
		co.cancel()
		co.cmd.Process.Kill()
		log.Infof("unloaded cog %q", cogPath)
	}()

	return nil
}

// ArgsType indicates the format that the args will be in.
type ArgsType int

const (
	// Unknown indicates that the type is unknown.
	Unknown ArgsType = 0

	// JSON indicates that the args are in JSON format.
	JSON ArgsType = 2
)

// Execute calls a Cog.
func (c *Client) Execute(ctx context.Context, cogPath, realUser string, args []byte, argsType pb.ArgsType, server *pb.Server) (pb.Status, []byte, error) {
	if server == nil {
		server = &pb.Server{}
	}

	log.Infof("before getCog")
	co, err := c.getCog(ctx, cogPath, false)
	if err != nil {
		return pb.Status_UNKNOWN, nil, err
	}
	log.Infof("after getCog")
	co.wg.Add(1)
	resp, err := co.client.Execute(
		ctx,
		&pb.ExecuteRequest{
			Args: &pb.Args{
				Args:     args,
				ArgsType: argsType,
			},
			RealUser: realUser,
			Server:   server,
			Token:    co.token,
		},
		grpc.FailFast(true),
	)
	co.wg.Done()

	if err != nil {
		log.Error(err)
		if co.cmd.ProcessState != nil {
			c.Unload(cogPath)
			return pb.Status_UNKNOWN, nil, CogCrashError(fmt.Sprintf("cog exited unexpectedly: %s", err))
		}
		log.Infof("before return")
		return pb.Status_UNKNOWN, nil, err
	}
	return resp.Out.Status, resp.Out.Output, nil
}

// Validate validates that the args with the Cog's Validate method. If the cog
// is not loaded it will be loaded.
func (c *Client) Validate(ctx context.Context, cogPath string, args []byte, argsType pb.ArgsType) error {
	co, err := c.getCog(ctx, cogPath, true)
	if err != nil {
		return err
	}

	co.wg.Add(1)
	_, err = co.client.Validate(
		ctx,
		&pb.ValidateRequest{
			Args: &pb.Args{
				Args:     args,
				ArgsType: argsType,
			},
		},
		grpc.FailFast(true),
	)
	co.wg.Done()

	return err
}

// Describe returns a description of the Cog.
func (c *Client) Describe(ctx context.Context, cogPath string) (*pb.Description, error) {
	co, err := c.getCog(ctx, cogPath, true)
	if err != nil {
		return nil, err
	}

	co.wg.Add(1)
	defer co.wg.Done()
	resp, err := co.client.Describe(ctx, &pb.DescribeRequest{}, grpc.FailFast(true))
	if err != nil {
		return nil, err
	}

	return resp.Description, nil
}

func (c *Client) getCog(ctx context.Context, cogPath string, load bool) (cogInfo, error) {
	// This funky lock crap is because we must hold the lock in order to
	// prevent an Unload() by anyone else before we increment the cogInfo.WaitGroup.
	log.Infof("before cogExist()")
	co, ok := c.cogExist(cogPath)
	log.Infof("after cogExist()")
	if !ok {
		if load {
			log.Infof("before c.Load()")
			if err := c.Load(ctx, cogPath); err != nil {
				return cogInfo{}, err
			}
		} else {
			return cogInfo{}, fmt.Errorf("cog %q is not loaded", cogPath)
		}
		log.Infof("before c.cogExist()")
		co, ok = c.cogExist(cogPath)
		if !ok {
			return cogInfo{}, fmt.Errorf("attempted load of cog, but it must have crashed")
		}
		return co, nil
	}

	// If the ProcessState != nil, this is an indication that the plugin died.
	if co.cmd.ProcessState != nil {
		log.Infof("before c.Unload()")
		c.Unload(cogPath)
		return cogInfo{}, CogCrashError("cog exited unexpectedly")
	}
	return co, nil
}

func (c *Client) cogExist(cogPath string) (cogInfo, bool) {
	c.cogsMu.Lock()
	defer c.cogsMu.Unlock()

	co, ok := c.cogs[cogPath]
	return co, ok
}

type socketInfo struct {
	err      error
	listener net.Listener
	addr     string
	token    []byte
}

func (c *Client) socketSetup() (string, chan socketInfo) {
	socketAddr := path.Join(os.TempDir(), "@"+uuid.New())
	ch := make(chan socketInfo, 1)

	go func() {
		log.Infof("before listen")
		l, err := net.Listen("unix", socketAddr)
		if err != nil {
			ch <- socketInfo{err: fmt.Errorf("listen error: %s", err)}
			return
		}

		log.Infof("before chmod")
		if err = os.Chmod(l.Addr().String(), 0700); err != nil {
			ch <- socketInfo{err: fmt.Errorf("could not chmod the domain socket: %s", err)}
			return
		}

		log.Infof("before accept")
		fd, err := l.Accept()
		if err != nil {
			ch <- socketInfo{err: fmt.Errorf("domain socket .Accept() error: %s", err)}
			return
		}

		log.Infof("reading size")
		size := int64(0)
		if err = binary.Read(fd, binary.BigEndian, &size); err != nil {
			ch <- socketInfo{err: fmt.Errorf("error reading address size: %s", err)}
			return
		}

		log.Infof("reading address")
		addr := make([]byte, size)
		if _, err = fd.Read(addr); err != nil {
			ch <- socketInfo{err: fmt.Errorf("error reading address: %s", err)}
			return
		}

		log.Infof("reading token size")
		if err = binary.Read(fd, binary.BigEndian, &size); err != nil {
			ch <- socketInfo{err: fmt.Errorf("error reading token size: %s", err)}
			return
		}

		log.Infof("reading token")
		token := make([]byte, size)
		if _, err := fd.Read(token); err != nil {
			ch <- socketInfo{err: fmt.Errorf("error reading token: %s", err)}
			return
		}

		log.Infof("send ack")
		if _, err := fd.Write([]byte("ack")); err != nil {
			ch <- socketInfo{err: fmt.Errorf("error writing ack: %s", err)}
			return
		}
		ch <- socketInfo{
			listener: l,
			addr:     string(addr),
			token:    token,
		}
	}()
	return socketAddr, ch
}
