/*
Package cog provides the framework for creating a Cog plugin. Cog plugins are
simply binaries that accept one arguments via the command line, argv[1] is a
unix port to listen on.  The plugin then brings up a GRPC service and
communicates what port it comes up on via the unix socket back to the caller.  A
security token is also exchanged to the client, which must used the token in
all GRPC communication with the plugin.
Cog plugins receive a proto.Message as its input.  They write back a
proto.Message that is sent back to the caller along with a status message and
optionally a pretty print message for display in things like a browser.
These plugins can then be dynamically loaded by calling applications and
provide hitless upgrades to various software stacks.
Here is a simple example that simply writes a status message of success back
to the caller:
	// cog merely implements the Cog interface called Cog.  The interface requires
	// Execute()/Describe()/Validate()/ArgsProto().
	type cog struct {}
	// Execute receives its arguments via "args", "out" represents the results of
	// the Cog's run.
	func (*cog) Execute(ctx context.Context, args proto.Message, out *pb.Out) error
		out.Status = pb.SUCCESS
		return nil
	}
	// Describe returns information about the plugin, including permissions and
	// tags to allow searching via an online repository.
	func (*cog) Describe() *pb.Description {
		return &pb.Description{
			Owner: ("doak"),
			Description: "Example plugin.",
			Tags: []string{"example"},
		}
	}
	// Validate validates the arguments that are sent or may be sent to
	// the plugin. Validate is always called during Execute(). You should never
	// invoke this in your code.
	func (*cog) Validate(args proto.Message) error {
    a := args.(myproto.Args)
	  // Check your arguments are valid here.
		return nil
	}
  // ArgsProto returns a blank copy of the argument proto you define. This
	// allows the Cog middleware to marshal/unmarshal JSON or Proto that is in
	// []byte form into your arguments. This saves you a lot of time and bother.
  func (*cog) ArgsProto() proto.Message {
    return &myproto.Args{}
  }
	func main() {
		runtime.GOMAXPROCS(runtime.NumCPU())
		if err := Start(&cog{}); err != nil {
			panic(err)
		}
		select{}
	}
*/
package cog

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/user"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	log "github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	proto "github.com/golang/protobuf/proto"
	pb "github.com/johnsiilver/cog/proto/cog"
)

var (
	// Note: I'm violating the --flag in main() on purpose. Every Cog should have this.
	cogDesc = flag.Bool("cog_desc", false, "The description of the Cog will be written out in proto text format and the binary will stop.")
)

var jsonMarshaller = &jsonpb.Marshaler{Indent: "\t"}

// Status represents the cog's return status.
type Status int

const (
	// Unknown indicates the plugin has not finished execution or error in
	// the plugin.
	Unknown Status = 0
	// Success indicates plugin succeeded.
	Success Status = 1
	// Failure indicates the plugin failed.
	Failure Status = 2
	// FailureNoRetry indicates the plugin failed and the calling system should
	// not retry with these arguments, as the plugin author determined that it
	// will continue to fail unless some change is made.
	FailureNoRetry Status = 3
)

// Out represents the output from the cog.
type Out struct {
	// Status is the status from the plugin.  Returned errors automatically
	// convert to Failure.
	Status Status

	// Output is output from the plugin.
	Output proto.Message
}

func (o Out) toProto(t pb.ArgsType) (*pb.Out, error) {
	p := &pb.Out{}

	switch t {
	case pb.ArgsType_JSON:
		if o.Output == nil {
			p.Output = []byte{}
			break
		}
		s, err := jsonMarshaller.MarshalToString(o.Output)
		if err != nil {
			return nil, err
		}
		p.Output = []byte(s)
	default:
		return nil, fmt.Errorf("cannot convert to pb.Out because ArgsType is %q", t)
	}

	switch o.Status {
	case Success:
		p.Status = pb.Status_SUCCESS
	case Failure:
		p.Status = pb.Status_FAILURE
	case FailureNoRetry:
		p.Status = pb.Status_FAILURE_NO_RETRIES
	default:
		return nil, fmt.Errorf("cannot convert to pb.Out because Status %d is not supported", o.Status)
	}
	return p, nil
}

// Cog represents the interface that all Cog plugins must satisfy.
type Cog interface {
	// Execute is used to execute the plugin.
	// args is the arguments to the plugin.
	// realUser is the user that the called the plugin.
	// marmotEndpoint is the service endpoint that is calling the plugin.
	// cogEndpoint is the Cog endpoint the Cog can utilize to get things from the server.
	Execute(ctx context.Context, args proto.Message, realUser, marmotEndpoint, cogEndpoint string) (Out, error)

	// Describe details information about the plugin along with usage restrictions.
	Describe() *pb.Description

	// Validate validates that the "p" argument would be accepted by the plugin.
	// This method is always called before Execute() is called, never
	// invoke it in the plugin.
	Validate(p proto.Message) error

	// ArgsProto returns an empty proto message used to represent the
	// arguments to an Execute() call with In.Args.
	ArgsProto() proto.Message
}

// service implements pb.CogService.
type service struct {
	// plugin holds the Cog instance that we use to process our RPCs.
	plugin Cog

	// user is the owner of the process.
	user string

	// sock is the path to unix domain socket used for communication.
	sock string

	// addr is the gRPC ip/port that the service is listening on.
	addr string

	// token is a security token that the client must always send the plugin
	// or the plugin crashes.
	token []byte
}

func (s *service) Execute(ctx context.Context, req *pb.ExecuteRequest) (*pb.ExecuteResponse, error) {
	if !bytes.Equal(req.Token, s.token) {
		panic("expected security token was not present from the client.  Third party hack attempt likely.")
	}

	args, err := s.inArgs(req.Args)
	if err != nil {
		return nil, err
	}

	err = s.plugin.Validate(args)
	if err != nil {
		return nil, err
	}

	var out Out

	done := make(chan error, 1)
	go func() {
		defer close(done)
		out, err = s.plugin.Execute(ctx, args, req.RealUser, req.Server.MarmotEndpoint, req.Server.CogEndpoint)
		if err != nil {
			done <- err
		}
	}()

	select {
	case <-ctx.Done():
		err = fmt.Errorf("Execute() did not complete before context deadline or the call was cancelled.")
		return nil, err
	case err = <-done:
		if err != nil {
			return nil, err
		}
	}

	p, err := out.toProto(req.Args.ArgsType)
	if err != nil {
		return nil, err
	}

	return &pb.ExecuteResponse{Out: p}, nil
}

// Execute implements pb.CogService.Describe.
func (s *service) Describe(ctx context.Context, req *pb.DescribeRequest) (*pb.DescribeResponse, error) {
	d := &pb.DescribeResponse{
		Description: s.plugin.Describe(),
	}
	if d.Description.MaxShutdownTime == 0 {
		d.Description.MaxShutdownTime = 30
	}
	return d, nil
}

// Validate implements pb.CogService.Validate.
func (s *service) Validate(ctx context.Context, req *pb.ValidateRequest) (*pb.ValidateResponse, error) {
	if req.Args == nil {
		return nil, fmt.Errorf("req.Args cannot be nil")
	}

	args, err := s.inArgs(req.Args)
	if err != nil {
		return nil, err
	}

	if err := s.plugin.Validate(args); err != nil {
		return nil, err
	}
	return &pb.ValidateResponse{}, nil
}

// inArgs translates in.Args textual representation into the proto.Message.
func (s *service) inArgs(in *pb.Args) (proto.Message, error) {
	if in == nil {
		return nil, fmt.Errorf("pb.Args cannot be nil")
	}

	args := s.plugin.ArgsProto()
	switch in.ArgsType {
	case pb.ArgsType_JSON:
		if err := json.Unmarshal(in.Args, args); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("ArgsType value %q is not supported", in.ArgsType)
	}
	return args, nil
}

// sockets returns the unix socket names required to start the plugin.
// Logs and exits if the socket names cannot be determined.
func (s *service) socket() error {
	if s.sock == "" {
		ln := len(os.Args)
		if ln < 2 {
			return fmt.Errorf("binary launched with incorrect arguments, should be: <command> <unix socket name>, got: %v", os.Args)
		}
		log.Infof("cog's args are: %#v", os.Args)
		log.Infof("socket is: %q", os.Args[1])

		s.sock = os.Args[1]
	}
	return nil
}

// StartOption is an optional argument for starting a Cog.
type StartOption func(s *service)

// Sock allows setting the unix socket name of which the plugin receives
// confirmation the client received information from the caller. By default
// it reads this from the command line arguments when starting up.
func Sock(p string) StartOption {
	return func(s *service) {
		s.sock = p
	}
}

// Start is used to start a cog instance.
func Start(c Cog, opts ...StartOption) error {
	if err := validateDescribe(c); err != nil {
		return err
	}

	// The user passed the -plugin_desc flag, which means we print out the
	// description proto to the screen and exit.
	if *cogDesc {
		fmt.Println(proto.MarshalTextString(c.Describe()))
		os.Exit(0)
	}

	u, err := user.Current()
	if err != nil {
		return err
	}

	s := &service{
		plugin: c,
		user:   u.Username,
		token:  token(),
	}

	for _, opt := range opts {
		opt(s)
	}

	s.socket()

	ipv4, ipv6, err := ipSupport()
	if err != nil {
		return fmt.Errorf("problem determining IP version support: %s", err)
	}

	local := ""
	if ipv4 {
		log.Infof("using localhost IPv4")
		local = "localhost:0"
	} else if ipv6 {
		log.Infof("using localhost IPv6")
		local = "[::1]:0"
	}

	lis, err := net.Listen("tcp", local)
	if err != nil {
		return fmt.Errorf("cog failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterCogServiceServer(grpcServer, s)
	go grpcServer.Serve(lis)

	s.addr = lis.Addr().String()

	log.Infof("cog is listening on %q", s.addr)

	uconn, err := net.Dial("unix", s.sock)
	if err != nil {
		return fmt.Errorf("could not dial unix socket %q: %s", s.sock, err)
	}
	defer uconn.Close()

	bAddr := []byte(s.addr)
	uconn.SetDeadline(time.Now().Add(30 * time.Second))
	log.Infof("sending size")
	if err := binary.Write(uconn, binary.BigEndian, int64(len(bAddr))); err != nil {
		return fmt.Errorf("error trying to send header: %s", err)
	}
	log.Infof("sending address")
	if _, err := uconn.Write(bAddr); err != nil {
		return fmt.Errorf("error trying to write address: %s", err)
	}

	log.Info("sending token size")
	if err := binary.Write(uconn, binary.BigEndian, int64(len(s.token))); err != nil {
		return fmt.Errorf("error trying to send token header: %s", err)
	}

	if _, err := uconn.Write(s.token); err != nil {
		return fmt.Errorf("error trying to write token: %s", err)
	}

	ack := make([]byte, len("ack"))
	log.Infof("waiting for ack")
	if _, err := uconn.Read(ack); err != nil {
		return fmt.Errorf("error waiting for ack message: %s", err)
	}

	// Launch a goroutine that polls for our parent process to die.  If it does,
	// we should exit.  We know this is the case if our parent process becomes 1.
	go func() {
		for _ = range time.Tick(5 * time.Second) {
			if os.Getppid() == 1 {
				log.Exitf("parent process died, exiting")
			}
		}
	}()

	return nil
}

var netInterfaces = net.Interfaces

// ipSupport determines if we support IPv4 and IPv6 loopback support.
// NOTE: There is no test here because net.Interfaces is untestable.  It has
// an .Addrs() method that returns a hidden global variable.  That obscures
// testing because we cannot fake that method's results without adding a lot of
// abstraction that isn't worth the trouble.
func ipSupport() (ipv4, ipv6 bool, err error) {
	ifaces, err := netInterfaces()
	if err != nil {
		return false, false, err
	}

	for _, i := range ifaces {
		if i.Flags&net.FlagLoopback != 0 {
			addrs, err := i.Addrs()
			if err != nil {
				return false, false, err
			}
			for _, addr := range addrs {
				var ip net.IP
				switch v := addr.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}

				if ip.To4() == nil {
					ipv6 = true
				} else {
					ipv4 = true
				}
			}
		}
	}
	return ipv6, ipv6, nil
}

// validateDescribe validates that the plugin has pluginerly used the
// Description proto message.
func validateDescribe(p Cog) error {
	m := p.Describe()

	if m.Owner == "" {
		return fmt.Errorf("Describe(): must return an Owner")
	}

	if m.Description == "" {
		return fmt.Errorf("Describe(): Description must be set")
	}

	shutTime := time.Duration(m.MaxShutdownTime) * time.Second

	if shutTime > 30*time.Minute {
		return fmt.Errorf("Describe(): MaxShutdownTime cannot exceed 30 minutes")
	}
	return nil
}

// token generates a crypto random 32 character token in []byte form.
func token() []byte {
	const size = 32

	rb := make([]byte, size)
	_, err := rand.Read(rb)
	if err != nil {
		panic(err)
	}

	return []byte(base64.URLEncoding.EncodeToString(rb))
}
