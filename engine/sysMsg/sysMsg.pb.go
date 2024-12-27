// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.33.0
// 	protoc        v5.27.3
// source: internal/sysMsg.proto

package sysMsg

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	anypb "google.golang.org/protobuf/types/known/anypb"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type SysMsgRedisKV struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Key    string     `protobuf:"bytes,1,opt,name=Key,proto3" json:"Key,omitempty"`
	Value  *anypb.Any `protobuf:"bytes,2,opt,name=Value,proto3" json:"Value,omitempty"`
	Expire int64      `protobuf:"varint,3,opt,name=Expire,proto3" json:"Expire,omitempty"`
}

func (x *SysMsgRedisKV) Reset() {
	*x = SysMsgRedisKV{}
	if protoimpl.UnsafeEnabled {
		mi := &file_internal_sysMsg_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *SysMsgRedisKV) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SysMsgRedisKV) ProtoMessage() {}

func (x *SysMsgRedisKV) ProtoReflect() protoreflect.Message {
	mi := &file_internal_sysMsg_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use SysMsgRedisKV.ProtoReflect.Descriptor instead.
func (*SysMsgRedisKV) Descriptor() ([]byte, []int) {
	return file_internal_sysMsg_proto_rawDescGZIP(), []int{0}
}

func (x *SysMsgRedisKV) GetKey() string {
	if x != nil {
		return x.Key
	}
	return ""
}

func (x *SysMsgRedisKV) GetValue() *anypb.Any {
	if x != nil {
		return x.Value
	}
	return nil
}

func (x *SysMsgRedisKV) GetExpire() int64 {
	if x != nil {
		return x.Expire
	}
	return 0
}

type SysMsgRedisString struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Value string `protobuf:"bytes,1,opt,name=Value,proto3" json:"Value,omitempty"`
}

func (x *SysMsgRedisString) Reset() {
	*x = SysMsgRedisString{}
	if protoimpl.UnsafeEnabled {
		mi := &file_internal_sysMsg_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *SysMsgRedisString) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SysMsgRedisString) ProtoMessage() {}

func (x *SysMsgRedisString) ProtoReflect() protoreflect.Message {
	mi := &file_internal_sysMsg_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use SysMsgRedisString.ProtoReflect.Descriptor instead.
func (*SysMsgRedisString) Descriptor() ([]byte, []int) {
	return file_internal_sysMsg_proto_rawDescGZIP(), []int{1}
}

func (x *SysMsgRedisString) GetValue() string {
	if x != nil {
		return x.Value
	}
	return ""
}

type SysMsgRedisKVWithoutExpire struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Key   string     `protobuf:"bytes,1,opt,name=Key,proto3" json:"Key,omitempty"`
	Value *anypb.Any `protobuf:"bytes,2,opt,name=Value,proto3" json:"Value,omitempty"`
}

func (x *SysMsgRedisKVWithoutExpire) Reset() {
	*x = SysMsgRedisKVWithoutExpire{}
	if protoimpl.UnsafeEnabled {
		mi := &file_internal_sysMsg_proto_msgTypes[2]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *SysMsgRedisKVWithoutExpire) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SysMsgRedisKVWithoutExpire) ProtoMessage() {}

func (x *SysMsgRedisKVWithoutExpire) ProtoReflect() protoreflect.Message {
	mi := &file_internal_sysMsg_proto_msgTypes[2]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use SysMsgRedisKVWithoutExpire.ProtoReflect.Descriptor instead.
func (*SysMsgRedisKVWithoutExpire) Descriptor() ([]byte, []int) {
	return file_internal_sysMsg_proto_rawDescGZIP(), []int{2}
}

func (x *SysMsgRedisKVWithoutExpire) GetKey() string {
	if x != nil {
		return x.Key
	}
	return ""
}

func (x *SysMsgRedisKVWithoutExpire) GetValue() *anypb.Any {
	if x != nil {
		return x.Value
	}
	return nil
}

var File_internal_sysMsg_proto protoreflect.FileDescriptor

var file_internal_sysMsg_proto_rawDesc = []byte{
	0x0a, 0x15, 0x69, 0x6e, 0x74, 0x65, 0x72, 0x6e, 0x61, 0x6c, 0x2f, 0x73, 0x79, 0x73, 0x4d, 0x73,
	0x67, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x06, 0x73, 0x79, 0x73, 0x4d, 0x73, 0x67, 0x1a,
	0x19, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66,
	0x2f, 0x61, 0x6e, 0x79, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x22, 0x65, 0x0a, 0x0d, 0x53, 0x79,
	0x73, 0x4d, 0x73, 0x67, 0x52, 0x65, 0x64, 0x69, 0x73, 0x4b, 0x56, 0x12, 0x10, 0x0a, 0x03, 0x4b,
	0x65, 0x79, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x03, 0x4b, 0x65, 0x79, 0x12, 0x2a, 0x0a,
	0x05, 0x56, 0x61, 0x6c, 0x75, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x14, 0x2e, 0x67,
	0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x41,
	0x6e, 0x79, 0x52, 0x05, 0x56, 0x61, 0x6c, 0x75, 0x65, 0x12, 0x16, 0x0a, 0x06, 0x45, 0x78, 0x70,
	0x69, 0x72, 0x65, 0x18, 0x03, 0x20, 0x01, 0x28, 0x03, 0x52, 0x06, 0x45, 0x78, 0x70, 0x69, 0x72,
	0x65, 0x22, 0x29, 0x0a, 0x11, 0x53, 0x79, 0x73, 0x4d, 0x73, 0x67, 0x52, 0x65, 0x64, 0x69, 0x73,
	0x53, 0x74, 0x72, 0x69, 0x6e, 0x67, 0x12, 0x14, 0x0a, 0x05, 0x56, 0x61, 0x6c, 0x75, 0x65, 0x18,
	0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x05, 0x56, 0x61, 0x6c, 0x75, 0x65, 0x22, 0x5a, 0x0a, 0x1a,
	0x53, 0x79, 0x73, 0x4d, 0x73, 0x67, 0x52, 0x65, 0x64, 0x69, 0x73, 0x4b, 0x56, 0x57, 0x69, 0x74,
	0x68, 0x6f, 0x75, 0x74, 0x45, 0x78, 0x70, 0x69, 0x72, 0x65, 0x12, 0x10, 0x0a, 0x03, 0x4b, 0x65,
	0x79, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x03, 0x4b, 0x65, 0x79, 0x12, 0x2a, 0x0a, 0x05,
	0x56, 0x61, 0x6c, 0x75, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x14, 0x2e, 0x67, 0x6f,
	0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x41, 0x6e,
	0x79, 0x52, 0x05, 0x56, 0x61, 0x6c, 0x75, 0x65, 0x42, 0x0a, 0x5a, 0x08, 0x2e, 0x3b, 0x73, 0x79,
	0x73, 0x4d, 0x73, 0x67, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_internal_sysMsg_proto_rawDescOnce sync.Once
	file_internal_sysMsg_proto_rawDescData = file_internal_sysMsg_proto_rawDesc
)

func file_internal_sysMsg_proto_rawDescGZIP() []byte {
	file_internal_sysMsg_proto_rawDescOnce.Do(func() {
		file_internal_sysMsg_proto_rawDescData = protoimpl.X.CompressGZIP(file_internal_sysMsg_proto_rawDescData)
	})
	return file_internal_sysMsg_proto_rawDescData
}

var file_internal_sysMsg_proto_msgTypes = make([]protoimpl.MessageInfo, 3)
var file_internal_sysMsg_proto_goTypes = []interface{}{
	(*SysMsgRedisKV)(nil),              // 0: sysMsg.SysMsgRedisKV
	(*SysMsgRedisString)(nil),          // 1: sysMsg.SysMsgRedisString
	(*SysMsgRedisKVWithoutExpire)(nil), // 2: sysMsg.SysMsgRedisKVWithoutExpire
	(*anypb.Any)(nil),                  // 3: google.protobuf.Any
}
var file_internal_sysMsg_proto_depIdxs = []int32{
	3, // 0: sysMsg.SysMsgRedisKV.Value:type_name -> google.protobuf.Any
	3, // 1: sysMsg.SysMsgRedisKVWithoutExpire.Value:type_name -> google.protobuf.Any
	2, // [2:2] is the sub-list for method output_type
	2, // [2:2] is the sub-list for method input_type
	2, // [2:2] is the sub-list for extension type_name
	2, // [2:2] is the sub-list for extension extendee
	0, // [0:2] is the sub-list for field type_name
}

func init() { file_internal_sysMsg_proto_init() }
func file_internal_sysMsg_proto_init() {
	if File_internal_sysMsg_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_internal_sysMsg_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*SysMsgRedisKV); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_internal_sysMsg_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*SysMsgRedisString); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_internal_sysMsg_proto_msgTypes[2].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*SysMsgRedisKVWithoutExpire); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_internal_sysMsg_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   3,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_internal_sysMsg_proto_goTypes,
		DependencyIndexes: file_internal_sysMsg_proto_depIdxs,
		MessageInfos:      file_internal_sysMsg_proto_msgTypes,
	}.Build()
	File_internal_sysMsg_proto = out.File
	file_internal_sysMsg_proto_rawDesc = nil
	file_internal_sysMsg_proto_goTypes = nil
	file_internal_sysMsg_proto_depIdxs = nil
}
