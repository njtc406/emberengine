package serializer

var (
	DefaultSerializerID int32
	serializers         []Serializer
)

func init() {
	RegisterSerializer(newProtoSerializer())
	RegisterSerializer(newJsonSerializer())
}

func RegisterSerializer(serializer Serializer) {
	serializers = append(serializers, serializer)
}

type Serializer interface {
	Serialize(msg interface{}) ([]byte, error)
	Deserialize(typeName string, bytes []byte) (interface{}, error)
	GetTypeName(msg interface{}) (string, error)
}

func Serialize(message interface{}, serializerID int32) ([]byte, string, error) {
	if message == nil {
		return nil, "", nil
	}
	res, err := serializers[serializerID].Serialize(message)
	if err != nil {
		return nil, "", err
	}
	typeName, err := serializers[serializerID].GetTypeName(message)
	if err != nil {
		return nil, "", err
	}
	return res, typeName, nil
}

func Deserialize(message []byte, typeName string, serializerID int32) (interface{}, error) {
	if message == nil || len(message) == 0 || typeName == "" {
		return nil, nil
	}
	return serializers[serializerID].Deserialize(typeName, message)
}
