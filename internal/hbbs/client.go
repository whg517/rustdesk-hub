package hbbs

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/whg517/rustdesk-hub/internal/rustdesk"
)

const (
	queryTimeout  = 2 * time.Second
	maxFrameBytes = 64 << 10
	MaxPeers      = 100

	// Field numbers from hbb_common's rendezvous.proto at the OSS 1.1.16 submodule revision.
	onlineRequest  = 23
	onlineResponse = 24
	requestPeers   = 2
	responseStates = 1
)

type Client struct {
	address string
}

// New accepts the ID server address as entered in the RustDesk client.
func New(idServer string) (*Client, error) {
	host, port, err := rustdesk.ParseServerAddress(idServer, rustdesk.IDPort)
	if err != nil {
		return nil, err
	}
	if port < 2 {
		return nil, errors.New("ID 服务器端口须在 2 至 65535 之间")
	}
	return &Client{address: net.JoinHostPort(host, strconv.Itoa(int(port)-1))}, nil
}

// Query returns one state per ID in the request order.
func (c *Client) Query(ctx context.Context, ids []string) ([]bool, error) {
	if len(ids) == 0 {
		return []bool{}, nil
	}
	if len(ids) > MaxPeers {
		return nil, fmt.Errorf("最多可查询 %d 个设备", MaxPeers)
	}

	var request []byte
	for _, id := range ids {
		request = protowire.AppendTag(request, requestPeers, protowire.BytesType)
		request = protowire.AppendString(request, id)
	}
	message := protowire.AppendTag(nil, onlineRequest, protowire.BytesType)
	message = protowire.AppendBytes(message, request)
	if len(message) > maxFrameBytes {
		return nil, errors.New("在线状态查询报文过大")
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", c.address)
	if err != nil {
		return nil, fmt.Errorf("连接 hbbs 在线状态端口 %w", err)
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	if err := writeFrame(conn, message); err != nil {
		return nil, fmt.Errorf("发送在线状态查询 %w", err)
	}
	reply, err := readFrame(conn)
	if err != nil {
		return nil, fmt.Errorf("读取在线状态响应 %w", err)
	}
	return decodeOnline(reply, len(ids))
}

func writeFrame(w io.Writer, payload []byte) error {
	headerSize := 1
	switch {
	case len(payload) >= 1<<14:
		headerSize = 3
	case len(payload) >= 1<<6:
		headerSize = 2
	}
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], uint32(len(payload))<<2|uint32(headerSize-1))
	frame := append(header[:headerSize:headerSize], payload...)
	n, err := w.Write(frame)
	if err == nil && n != len(frame) {
		return io.ErrShortWrite
	}
	return err
}

func readFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:1]); err != nil {
		return nil, err
	}
	headerSize := int(header[0]&3) + 1
	if _, err := io.ReadFull(r, header[1:headerSize]); err != nil {
		return nil, err
	}
	length := binary.LittleEndian.Uint32(header[:]) >> 2
	if length == 0 || length > maxFrameBytes {
		return nil, fmt.Errorf("hbbs 响应帧大小无效 %d", length)
	}
	message := make([]byte, length)
	_, err := io.ReadFull(r, message)
	return message, err
}

func bytesField(message []byte, field protowire.Number) ([]byte, error) {
	var result []byte
	for len(message) > 0 {
		number, typ, tagSize := protowire.ConsumeTag(message)
		if tagSize < 0 {
			return nil, protowire.ParseError(tagSize)
		}
		message = message[tagSize:]
		if number == field && typ == protowire.BytesType {
			value, size := protowire.ConsumeBytes(message)
			if size < 0 {
				return nil, protowire.ParseError(size)
			}
			result = value
			message = message[size:]
			continue
		}
		size := protowire.ConsumeFieldValue(number, typ, message)
		if size < 0 {
			return nil, protowire.ParseError(size)
		}
		message = message[size:]
	}
	if result == nil {
		return nil, fmt.Errorf("hbbs 响应缺少字段 %d", field)
	}
	return result, nil
}

func decodeOnline(message []byte, count int) ([]bool, error) {
	response, err := bytesField(message, onlineResponse)
	if err != nil {
		return nil, err
	}
	states, err := bytesField(response, responseStates)
	if err != nil {
		return nil, err
	}
	if len(states) != (count+7)/8 {
		return nil, errors.New("hbbs 返回的在线状态数量不匹配")
	}

	online := make([]bool, count)
	for i := range online {
		// hbbs 1.1.16 reports peers from the most significant bit of each byte.
		online[i] = states[i/8]&(0x80>>uint(i%8)) != 0
	}
	return online, nil
}
