package serializer

import (
	"encoding/json"
	"fmt"
	"github.com/gogo/protobuf/proto"

	"reflect"
)

type jsonSerializer struct {
}

func newJsonSerializer() Serializer {
	return &jsonSerializer{}
}

func (j *jsonSerializer) Serialize(msg interface{}) ([]byte, error) {
	if message, ok := msg.(*JsonMessage); ok {
		return []byte(message.Json), nil
	} else if message, ok := msg.(proto.Message); ok {

		str, err := json.Marshal(message)
		if err != nil {
			return nil, err
		}

		return []byte(str), nil
	}
	return nil, fmt.Errorf("msg must be proto.Message")
}

func (j *jsonSerializer) Deserialize(typeName string, b []byte) (interface{}, error) {
	protoType := proto.MessageType(typeName)
	if protoType == nil {
		m := &JsonMessage{
			TypeName: typeName,
			Json:     string(b),
		}
		return m, nil
	}
	t := protoType.Elem()

	intPtr := reflect.New(t)
	instance, ok := intPtr.Interface().(proto.Message)
	if ok {
		json.Unmarshal(b, instance)
		return instance, nil
	}

	return nil, fmt.Errorf("msg must be proto.Message")
}

func (j *jsonSerializer) GetTypeName(msg interface{}) (string, error) {
	if message, ok := msg.(*JsonMessage); ok {
		return message.TypeName, nil
	} else if message, ok := msg.(proto.Message); ok {
		typeName := proto.MessageName(message)

		return typeName, nil
	}

	return "", fmt.Errorf("msg must be proto.Message")
}
