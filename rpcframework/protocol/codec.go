package protocol

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var (
	marshalOptions = &protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: true,
		UseEnumNumbers:  false,
		Indent:          "",
		AllowPartial:    true,
	}
	unmarshalOptions = &protojson.UnmarshalOptions{
		DiscardUnknown: true,
		AllowPartial:   true,
	}
)

// ProtobufCodec: Protobuf Codec.
type ProtobufCodec struct{}

// DecodeProtobufMessage decodes protobuf message.
func (c *ProtobufCodec) Decode(data []byte, msg proto.Message) error {
	// use protojson to decode
	if err := unmarshalOptions.Unmarshal(data, msg); err != nil {
		return fmt.Errorf("failed to unmarshal protobuf: %w", err)
	}

	return nil
}

// EncodeProtobufMessage encodes protobuf message.
func (c *ProtobufCodec) Encode(msg proto.Message) (json.RawMessage, error) {
	// use protojson to encode
	data, err := marshalOptions.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal protobuf: %w", err)
	}

	return data, nil
}
