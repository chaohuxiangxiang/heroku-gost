// SOCKS Protocol Version 5
// http://tools.ietf.org/html/rfc1928
// http://tools.ietf.org/html/rfc1929
package gosocks5

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	//"log"
	"net"
	"strconv"
)

const (
	Ver5        = 5
	UserPassVer = 1
)

const (
	MethodNoAuth uint8 = iota
	MethodGSSAPI
	MethodUserPass
	// X'03' to X'7F' IANA ASSIGNED
	// X'80' to X'FE' RESERVED FOR PRIVATE METHODS
	MethodNoAcceptable = 0xFF
)

const (
	CmdConnect uint8 = 1
	CmdBind          = 2
	CmdUdp           = 3
)

const (
	AddrIPv4   uint8 = 1
	AddrDomain       = 3
	AddrIPv6         = 4
)

const (
	Succeeded uint8 = iota
	Failure
	NotAllowed
	NetUnreachable
	HostUnreachable
	ConnRefused
	TTLExpired
	CmdUnsupported
	AddrUnsupported
)

var (
	ErrBadVersion  = errors.New("Bad version")
	ErrBadFormat   = errors.New("Bad format")
	ErrBadAddrType = errors.New("Bad address type")
	ErrShortBuffer = errors.New("Short buffer")
	ErrBadMethod   = errors.New("Bad method")
	ErrAuthFailure = errors.New("Auth failure")
)

/*
Method selection
+----+----------+----------+
|VER | NMETHODS | METHODS  |
+----+----------+----------+
| 1  |    1     | 1 to 255 |
+----+----------+----------+
*/
func ReadMethods(r io.Reader) ([]uint8, error) {
	b := make([]byte, 257)
	n, err := io.ReadAtLeast(r, b, 2)
	if err != nil {
		return nil, err
	}

	if b[0] != Ver5 {
		return nil, ErrBadVersion
	}

	if b[1] == 0 {
		return nil, ErrBadMethod
	}

	length := 2 + int(b[1])
	if n < length {
		if _, err := io.ReadFull(r, b[n:length]); err != nil {
			return nil, err
		}
	}

	return b[2:length], nil
}

func WriteMethod(method uint8, w io.Writer) error {
	_, err := w.Write([]byte{Ver5, method})
	return err
}

/*
 Username/Password authentication request
 +----+------+----------+------+----------+
 |VER | ULEN |  UNAME   | PLEN |  PASSWD  |
 +----+------+----------+------+----------+
 | 1  |  1   | 1 to 255 |  1   | 1 to 255 |
 +----+------+----------+------+----------+
*/
type UserPassRequest struct {
	Version  byte
	Username string
	Password string
}

func NewUserPassRequest(ver byte, u, p string) *UserPassRequest {
	return &UserPassRequest{
		Version:  ver,
		Username: u,
		Password: p,
	}
}

func ReadUserPassRequest(r io.Reader) (*UserPassRequest, error) {
	b := make([]byte, 513)
	n, err := io.ReadAtLeast(r, b, 2)
	if err != nil {
		return nil, err
	}

	if b[0] != UserPassVer {
		return nil, ErrBadVersion
	}

	req := &UserPassRequest{
		Version: b[0],
	}

	ulen := int(b[1])
	length := ulen + 3

	if n < length {
		if _, err := io.ReadFull(r, b[n:length]); err != nil {
			return nil, err
		}
		n = length
	}
	req.Username = string(b[2 : 2+ulen])

	plen := int(b[length-1])
	length += plen
	if n < length {
		if _, err := io.ReadFull(r, b[n:length]); err != nil {
			return nil, err
		}
	}
	req.Password = string(b[3+ulen : length])
	return req, nil
}

func (req *UserPassRequest) Write(w io.Writer) error {
	b := make([]byte, 513)
	b[0] = req.Version
	ulen := len(req.Username)
	b[1] = byte(ulen)
	length := 2 + ulen
	copy(b[2:length], req.Username)

	plen := len(req.Password)
	b[length] = byte(plen)
	length++
	copy(b[length:length+plen], req.Password)
	length += plen

	_, err := w.Write(b[:length])
	return err
}

/*
 Username/Password authentication response
 +----+--------+
 |VER | STATUS |
 +----+--------+
 | 1  |   1    |
 +----+--------+
*/
type UserPassResponse struct {
	Version byte
	Status  byte
}

func NewUserPassResponse(ver, status byte) *UserPassResponse {
	return &UserPassResponse{
		Version: ver,
		Status:  status,
	}
}

func ReadUserPassResponse(r io.Reader) (*UserPassResponse, error) {
	b := make([]byte, 2)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, err
	}

	if b[0] != UserPassVer {
		return nil, ErrBadVersion
	}

	res := &UserPassResponse{
		Version: b[0],
		Status:  b[1],
	}

	return res, nil
}

func (res *UserPassResponse) Write(w io.Writer) error {
	_, err := w.Write([]byte{res.Version, res.Status})
	return err
}

type Addr struct {
	Type uint8
	Host string
	Port uint16
}

func (addr *Addr) Decode(b []byte) error {
	addr.Type = b[0]
	pos := 1
	switch addr.Type {
	case AddrIPv4:
		addr.Host = net.IP(b[pos : pos+net.IPv4len]).String()
		pos += net.IPv4len
	case AddrIPv6:
		addr.Host = net.IP(b[pos : pos+net.IPv6len]).String()
		pos += net.IPv6len
	case AddrDomain:
		addrlen := int(b[pos])
		pos++
		addr.Host = string(b[pos : pos+addrlen])
		pos += addrlen
	default:
		return ErrBadAddrType
	}

	addr.Port = binary.BigEndian.Uint16(b[pos:])

	return nil
}

func (addr *Addr) Encode(b []byte) (int, error) {
	b[0] = addr.Type
	pos := 1
	switch addr.Type {
	case AddrIPv4:
		pos += copy(b[pos:], net.ParseIP(addr.Host).To4())
	case AddrDomain:
		b[pos] = byte(len(addr.Host))
		pos++
		pos += copy(b[pos:], []byte(addr.Host))
	case AddrIPv6:
		pos += copy(b[pos:], net.ParseIP(addr.Host).To16())
	default:
		b[0] = AddrIPv4
		pos += 4
	}
	binary.BigEndian.PutUint16(b[pos:], addr.Port)
	pos += 2

	return pos, nil
}

func (addr *Addr) String() string {
	return net.JoinHostPort(addr.Host, strconv.Itoa(int(addr.Port)))
}

/*
The SOCKSv5 request
+----+-----+-------+------+----------+----------+
|VER | CMD |  RSV  | ATYP | DST.ADDR | DST.PORT |
+----+-----+-------+------+----------+----------+
| 1  |  1  | X'00' |  1   | Variable |    2     |
+----+-----+-------+------+----------+----------+
*/
type Request struct {
	Cmd  uint8
	Addr *Addr
}

func NewRequest(cmd uint8, addr *Addr) *Request {
	return &Request{
		Cmd:  cmd,
		Addr: addr,
	}
}

func ReadRequest(r io.Reader) (*Request, error) {
	b := make([]byte, 262)
	n, err := io.ReadAtLeast(r, b, 5)
	if err != nil {
		return nil, err
	}

	if b[0] != Ver5 {
		return nil, ErrBadVersion
	}

	request := &Request{
		Cmd: b[1],
	}

	atype := b[3]
	length := 0
	switch atype {
	case AddrIPv4:
		length = 10
	case AddrIPv6:
		length = 22
	case AddrDomain:
		length = 7 + int(b[4])
	default:
		return nil, ErrBadAddrType
	}

	if n < length {
		if _, err := io.ReadFull(r, b[n:length]); err != nil {
			return nil, err
		}
	}
	addr := new(Addr)
	if err := addr.Decode(b[3:length]); err != nil {
		return nil, err
	}
	request.Addr = addr

	return request, nil
}

func (r *Request) Write(w io.Writer) (err error) {
	b := make([]byte, 262)

	b[0] = Ver5
	b[1] = r.Cmd
	// b[2] = 0 //rsv
	b[3] = AddrIPv4 // default

	length := 10
	if r.Addr != nil {
		n, _ := r.Addr.Encode(b[3:])
		length = 3 + n
	}
	_, err = w.Write(b[:length])
	return
}

func (r *Request) String() string {
	return fmt.Sprintf("5 %d 0 %d %s",
		r.Cmd, r.Addr.Type, r.Addr.String())
}

/*
The SOCKSv5 reply
+----+-----+-------+------+----------+----------+
|VER | REP |  RSV  | ATYP | BND.ADDR | BND.PORT |
+----+-----+-------+------+----------+----------+
| 1  |  1  | X'00' |  1   | Variable |    2     |
+----+-----+-------+------+----------+----------+
*/
type Reply struct {
	Rep  uint8
	Addr *Addr
}

func NewReply(rep uint8, addr *Addr) *Reply {
	return &Reply{
		Rep:  rep,
		Addr: addr,
	}
}

func ReadReply(r io.Reader) (*Reply, error) {
	b := make([]byte, 262)
	n, err := io.ReadAtLeast(r, b, 5)
	if err != nil {
		return nil, err
	}

	if b[0] != Ver5 {
		return nil, ErrBadVersion
	}

	reply := &Reply{
		Rep: b[1],
	}

	atype := b[3]
	length := 0
	switch atype {
	case AddrIPv4:
		length = 10
	case AddrIPv6:
		length = 22
	case AddrDomain:
		length = 7 + int(b[4])
	default:
		return nil, ErrBadAddrType
	}

	if n < length {
		if _, err := io.ReadFull(r, b[n:length]); err != nil {
			return nil, err
		}
	}

	addr := new(Addr)
	if err := addr.Decode(b[3:length]); err != nil {
		return nil, err
	}
	reply.Addr = addr

	return reply, nil
}

func (r *Reply) Write(w io.Writer) (err error) {
	b := make([]byte, 262)

	b[0] = Ver5
	b[1] = r.Rep
	// b[2] = 0 //rsv
	b[3] = AddrIPv4 // default

	length := 10
	if r.Addr != nil {
		n, _ := r.Addr.Encode(b[3:])
		length = 3 + n
	}
	_, err = w.Write(b[:length])

	return
}

func (r *Reply) String() string {
	return fmt.Sprintf("5 %d 0 %d %s",
		r.Rep, r.Addr.Type, r.Addr.String())
}

/*
UDP request
+----+------+------+----------+----------+----------+
|RSV | FRAG | ATYP | DST.ADDR | DST.PORT |   DATA   |
+----+------+------+----------+----------+----------+
| 2  |  1   |  1   | Variable |    2     | Variable |
+----+------+------+----------+----------+----------+
*/
type UDPHeader struct {
	Rsv  uint16
	Frag uint8
	Addr *Addr
}

func NewUDPHeader(rsv uint16, frag uint8, addr *Addr) *UDPHeader {
	return &UDPHeader{
		Rsv:  rsv,
		Frag: frag,
		Addr: addr,
	}
}

func (h *UDPHeader) String() string {
	return fmt.Sprintf("%d %d %d %s",
		h.Rsv, h.Frag, h.Addr.Type, h.Addr.String())
}

type UDPDatagram struct {
	Header *UDPHeader
	Data   []byte
}

func NewUDPDatagram(header *UDPHeader, data []byte) *UDPDatagram {
	return &UDPDatagram{
		Header: header,
		Data:   data,
	}
}

func ReadUDPDatagram(r io.Reader) (*UDPDatagram, error) {
	b := make([]byte, 65797)
	n, err := io.ReadAtLeast(r, b, 5)
	if err != nil {
		return nil, err
	}

	header := &UDPHeader{
		Rsv:  binary.BigEndian.Uint16(b[:2]),
		Frag: b[2],
	}

	atype := b[3]
	hlen := 0
	switch atype {
	case AddrIPv4:
		hlen = 10
	case AddrIPv6:
		hlen = 22
	case AddrDomain:
		hlen = 7 + int(b[4])
	default:
		return nil, ErrBadAddrType
	}

	dlen := int(header.Rsv)
	if n < hlen+dlen {
		if _, err := io.ReadFull(r, b[n:hlen+dlen]); err != nil {
			return nil, err
		}
		n = hlen + dlen
	}

	header.Addr = new(Addr)
	if err := header.Addr.Decode(b[3:hlen]); err != nil {
		return nil, err
	}

	d := &UDPDatagram{
		Header: header,
		Data:   b[hlen:n],
	}

	return d, nil
}

func (d *UDPDatagram) Write(w io.Writer) error {
	buffer := &bytes.Buffer{}

	b := make([]byte, 259)
	if d.Header != nil {
		binary.BigEndian.PutUint16(b[:2], d.Header.Rsv)
		buffer.Write(b[:2])
		buffer.WriteByte(d.Header.Frag)

		b[0] = AddrIPv4
		b[1] = 0
		length := 7

		if d.Header.Addr != nil {
			length, _ = d.Header.Addr.Encode(b)
		}
		buffer.Write(b[:length])
	} else {
		b[3] = AddrIPv4
		buffer.Write(b[:10])
	}

	buffer.Write(d.Data)
	_, err := w.Write(buffer.Bytes())

	return err
}
