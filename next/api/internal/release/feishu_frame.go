package release

import (
	"encoding/binary"
	"errors"
)

// feishuFrame is one frame on Feishu's long connection, a protobuf message (pbbp2.Frame in
// Feishu's SDKs). Method 0 is a control frame (ping, pong), 1 a data frame (an event); the type
// header says which.
type feishuFrame struct {
	seqID, logID    uint64
	service, method int32
	headers         [][2]string
	payloadEncoding string
	payloadType     string
	payload         []byte
	logIDNew        string
}

const (
	feishuControl = 0
	feishuData    = 1
)

func (f feishuFrame) header(key string) string {
	for _, h := range f.headers {
		if h[0] == key {
			return h[1]
		}
	}
	return ""
}

func appendTag(b []byte, field int, wire int) []byte {
	return binary.AppendUvarint(b, uint64(field<<3|wire))
}

func appendBytes(b []byte, field int, value []byte) []byte {
	b = appendTag(b, field, 2)
	b = binary.AppendUvarint(b, uint64(len(value)))
	return append(b, value...)
}

func (f feishuFrame) marshal() []byte {
	var b []byte
	b = binary.AppendUvarint(appendTag(b, 1, 0), f.seqID)
	b = binary.AppendUvarint(appendTag(b, 2, 0), f.logID)
	b = binary.AppendUvarint(appendTag(b, 3, 0), uint64(int64(f.service)))
	b = binary.AppendUvarint(appendTag(b, 4, 0), uint64(int64(f.method)))
	for _, h := range f.headers {
		header := appendBytes(appendBytes(nil, 1, []byte(h[0])), 2, []byte(h[1]))
		b = appendBytes(b, 5, header)
	}
	// As Feishu's SDKs do: the optional strings always, the payload when there is one.
	b = appendBytes(b, 6, []byte(f.payloadEncoding))
	b = appendBytes(b, 7, []byte(f.payloadType))
	if f.payload != nil {
		b = appendBytes(b, 8, f.payload)
	}
	return appendBytes(b, 9, []byte(f.logIDNew))
}

var errFeishuFrame = errors.New("malformed Feishu frame")

// protoFields walks a protobuf message, calling visit with each field's number and its varint or
// bytes; fixed-width fields are skipped, as the frame has none.
func protoFields(data []byte, visit func(field int, varint uint64, bytes []byte) error) error {
	for len(data) > 0 {
		key, n := binary.Uvarint(data)
		if n <= 0 {
			return errFeishuFrame
		}
		data = data[n:]
		field, wire := int(key>>3), key&7
		switch wire {
		case 0:
			v, n := binary.Uvarint(data)
			if n <= 0 {
				return errFeishuFrame
			}
			data = data[n:]
			if err := visit(field, v, nil); err != nil {
				return err
			}
		case 2:
			size, n := binary.Uvarint(data)
			if n <= 0 || uint64(len(data)-n) < size {
				return errFeishuFrame
			}
			value := data[n : n+int(size)]
			data = data[n+int(size):]
			if err := visit(field, 0, value); err != nil {
				return err
			}
		case 1:
			if len(data) < 8 {
				return errFeishuFrame
			}
			data = data[8:]
		case 5:
			if len(data) < 4 {
				return errFeishuFrame
			}
			data = data[4:]
		default:
			return errFeishuFrame
		}
	}
	return nil
}

func unmarshalFeishuFrame(data []byte) (feishuFrame, error) {
	var f feishuFrame
	err := protoFields(data, func(field int, v uint64, b []byte) error {
		switch field {
		case 1:
			f.seqID = v
		case 2:
			f.logID = v
		case 3:
			f.service = int32(v)
		case 4:
			f.method = int32(v)
		case 5:
			var h [2]string
			if err := protoFields(b, func(field int, _ uint64, b []byte) error {
				if field == 1 || field == 2 {
					h[field-1] = string(b)
				}
				return nil
			}); err != nil {
				return err
			}
			f.headers = append(f.headers, h)
		case 6:
			f.payloadEncoding = string(b)
		case 7:
			f.payloadType = string(b)
		case 8:
			f.payload = append([]byte(nil), b...)
		case 9:
			f.logIDNew = string(b)
		}
		return nil
	})
	return f, err
}
