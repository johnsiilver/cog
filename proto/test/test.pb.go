// Code generated by protoc-gen-go.
// source: test.proto
// DO NOT EDIT!

/*
Package cog_test is a generated protocol buffer package.

It is generated from these files:
	test.proto

It has these top-level messages:
	Args
	Response
*/
package cog_test

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

type Args struct {
	Say string `protobuf:"bytes,1,opt,name=Say" json:"Say,omitempty"`
}

func (m *Args) Reset()                    { *m = Args{} }
func (m *Args) String() string            { return proto.CompactTextString(m) }
func (*Args) ProtoMessage()               {}
func (*Args) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{0} }

type Response struct {
	Answer string `protobuf:"bytes,1,opt,name=Answer" json:"Answer,omitempty"`
}

func (m *Response) Reset()                    { *m = Response{} }
func (m *Response) String() string            { return proto.CompactTextString(m) }
func (*Response) ProtoMessage()               {}
func (*Response) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{1} }

func init() {
	proto.RegisterType((*Args)(nil), "cog_test.Args")
	proto.RegisterType((*Response)(nil), "cog_test.Response")
}

func init() { proto.RegisterFile("test.proto", fileDescriptor0) }

var fileDescriptor0 = []byte{
	// 98 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x09, 0x6e, 0x88, 0x02, 0xff, 0xe2, 0xe2, 0x2a, 0x49, 0x2d, 0x2e,
	0xd1, 0x2b, 0x28, 0xca, 0x2f, 0xc9, 0x17, 0xe2, 0x48, 0xce, 0x4f, 0x8f, 0x07, 0xf1, 0x95, 0x24,
	0xb8, 0x58, 0x1c, 0x8b, 0xd2, 0x8b, 0x85, 0x04, 0xb8, 0x98, 0x83, 0x13, 0x2b, 0x25, 0x18, 0x15,
	0x18, 0x35, 0x38, 0x83, 0x40, 0x4c, 0x25, 0x25, 0x2e, 0x8e, 0xa0, 0xd4, 0xe2, 0x82, 0xfc, 0xbc,
	0xe2, 0x54, 0x21, 0x31, 0x2e, 0x36, 0xc7, 0xbc, 0xe2, 0xf2, 0xd4, 0x22, 0xa8, 0x02, 0x28, 0x2f,
	0x89, 0x0d, 0x6c, 0x9c, 0x31, 0x20, 0x00, 0x00, 0xff, 0xff, 0x26, 0x45, 0xf5, 0x40, 0x5c, 0x00,
	0x00, 0x00,
}
