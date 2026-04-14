package tunnel

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const (
	managedQUICALPN = "gps-tunnel-quic/1"

	quicControlTypeRegister = "register"
	quicControlTypeAck      = "ack"
	quicControlTypeError    = "error"

	quicStreamTypeTCP = "tcp_open"

	quicDatagramTypeServerToClient byte = 1
	quicDatagramTypeClientToServer byte = 2
	quicDatagramTypeClose          byte = 3
)

type quicControlRegister struct {
	Type       string `json:"type"`
	Token      string `json:"token"`
	ClientName string `json:"clientName"`
}

type quicControlAck struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type quicTCPStreamOpenRequest struct {
	Type         string `json:"type"`
	RouteName    string `json:"routeName"`
	ConnectionID string `json:"connectionId"`
	SourceAddr   string `json:"sourceAddr"`
	PublicPort   int    `json:"publicPort"`
}

type quicUDPServerDatagram struct {
	SessionID  string
	RouteName  string
	SourceAddr string
	Payload    []byte
}

type quicUDPClientDatagram struct {
	SessionID string
	Payload   []byte
	Close     bool
}

func cloneBytes(src []byte) []byte {
	if len(src) == 0 {
		return nil
	}
	cloned := make([]byte, len(src))
	copy(cloned, src)
	return cloned
}

func marshalQUICUDPServerDatagram(frame quicUDPServerDatagram) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 1+2+len(frame.SessionID)+2+len(frame.RouteName)+2+len(frame.SourceAddr)+2+len(frame.Payload)))
	buf.WriteByte(quicDatagramTypeServerToClient)
	if err := writeFrameString(buf, frame.SessionID); err != nil {
		return nil, err
	}
	if err := writeFrameString(buf, frame.RouteName); err != nil {
		return nil, err
	}
	if err := writeFrameString(buf, frame.SourceAddr); err != nil {
		return nil, err
	}
	if err := writeFrameBytes(buf, frame.Payload); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func unmarshalQUICUDPServerDatagram(data []byte) (quicUDPServerDatagram, error) {
	var frame quicUDPServerDatagram
	reader := bytes.NewReader(data)
	msgType, err := reader.ReadByte()
	if err != nil {
		return frame, fmt.Errorf("read datagram type: %w", err)
	}
	if msgType != quicDatagramTypeServerToClient {
		return frame, fmt.Errorf("unexpected datagram type %d", msgType)
	}
	if frame.SessionID, err = readFrameString(reader); err != nil {
		return frame, err
	}
	if frame.RouteName, err = readFrameString(reader); err != nil {
		return frame, err
	}
	if frame.SourceAddr, err = readFrameString(reader); err != nil {
		return frame, err
	}
	if frame.Payload, err = readFrameBytes(reader); err != nil {
		return frame, err
	}
	return frame, nil
}

func marshalQUICUDPClientDatagram(frame quicUDPClientDatagram) ([]byte, error) {
	msgType := quicDatagramTypeClientToServer
	if frame.Close {
		msgType = quicDatagramTypeClose
	}
	buf := bytes.NewBuffer(make([]byte, 0, 1+2+len(frame.SessionID)+2+len(frame.Payload)))
	buf.WriteByte(msgType)
	if err := writeFrameString(buf, frame.SessionID); err != nil {
		return nil, err
	}
	if !frame.Close {
		if err := writeFrameBytes(buf, frame.Payload); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func unmarshalQUICUDPClientDatagram(data []byte) (quicUDPClientDatagram, error) {
	var frame quicUDPClientDatagram
	reader := bytes.NewReader(data)
	msgType, err := reader.ReadByte()
	if err != nil {
		return frame, fmt.Errorf("read datagram type: %w", err)
	}
	if msgType != quicDatagramTypeClientToServer && msgType != quicDatagramTypeClose {
		return frame, fmt.Errorf("unexpected datagram type %d", msgType)
	}
	frame.Close = msgType == quicDatagramTypeClose
	if frame.SessionID, err = readFrameString(reader); err != nil {
		return frame, err
	}
	if !frame.Close {
		if frame.Payload, err = readFrameBytes(reader); err != nil {
			return frame, err
		}
	}
	return frame, nil
}

func writeFrameString(buf *bytes.Buffer, value string) error {
	if len(value) > 0xffff {
		return fmt.Errorf("frame string too large")
	}
	if err := binary.Write(buf, binary.BigEndian, uint16(len(value))); err != nil {
		return fmt.Errorf("write frame string length: %w", err)
	}
	_, _ = buf.WriteString(value)
	return nil
}

func writeFrameBytes(buf *bytes.Buffer, value []byte) error {
	if len(value) > 0xffff {
		return fmt.Errorf("frame payload too large")
	}
	if err := binary.Write(buf, binary.BigEndian, uint16(len(value))); err != nil {
		return fmt.Errorf("write frame payload length: %w", err)
	}
	_, _ = buf.Write(value)
	return nil
}

func readFrameString(reader *bytes.Reader) (string, error) {
	data, err := readFrameBytes(reader)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func readFrameBytes(reader *bytes.Reader) ([]byte, error) {
	var length uint16
	if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
		return nil, fmt.Errorf("read frame length: %w", err)
	}
	payload := make([]byte, length)
	if _, err := reader.Read(payload); err != nil {
		return nil, fmt.Errorf("read frame payload: %w", err)
	}
	return payload, nil
}
