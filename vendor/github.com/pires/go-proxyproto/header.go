// Package proxyproto implements Proxy Protocol (v1 and v2) parser and writer, as per specification:
// http://www.haproxy.org/download/1.5/doc/proxy-protocol.txt
package proxyproto

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"time"
)

var (
	// Protocol
	SIGV1 = []byte{'\x50', '\x52', '\x4F', '\x58', '\x59'}
	SIGV2 = []byte{'\x0D', '\x0A', '\x0D', '\x0A', '\x00', '\x0D', '\x0A', '\x51', '\x55', '\x49', '\x54', '\x0A'}

	ErrCantReadProtocolVersionAndCommand    = errors.New("proxyproto: can't read proxy protocol version and command")
	ErrCantReadAddressFamilyAndProtocol     = errors.New("proxyproto: can't read address family or protocol")
	ErrCantReadLength                       = errors.New("proxyproto: can't read length")
	ErrCantResolveSourceUnixAddress         = errors.New("proxyproto: can't resolve source Unix address")
	ErrCantResolveDestinationUnixAddress    = errors.New("proxyproto: can't resolve destination Unix address")
	ErrNoProxyProtocol                      = errors.New("proxyproto: proxy protocol signature not present")
	ErrUnknownProxyProtocolVersion          = errors.New("proxyproto: unknown proxy protocol version")
	ErrUnsupportedProtocolVersionAndCommand = errors.New("proxyproto: unsupported proxy protocol version and command")
	ErrUnsupportedAddressFamilyAndProtocol  = errors.New("proxyproto: unsupported address family and protocol")
	ErrInvalidLength                        = errors.New("proxyproto: invalid length")
	ErrInvalidAddress                       = errors.New("proxyproto: invalid address")
	ErrInvalidPortNumber                    = errors.New("proxyproto: invalid port number")
	ErrSuperfluousProxyHeader               = errors.New("proxyproto: upstream connection sent PROXY header but isn't allowed to send one")
)

// Header is the placeholder for proxy protocol header.
type Header struct {
	Version            byte
	Command            ProtocolVersionAndCommand
	TransportProtocol  AddressFamilyAndProtocol
	SourceAddress      net.IP
	DestinationAddress net.IP
	SourcePort         uint16
	DestinationPort    uint16
	rawTLVs            []byte
}

// HeaderProxyFromAddrs creates a new PROXY header from a source and a
// destination address. If version is zero, the latest protocol version is
// used.
//
// The header is filled on a best-effort basis: if hints cannot be inferred
// from the provided addresses, the header will be left unspecified.
func HeaderProxyFromAddrs(version byte, sourceAddr, destAddr net.Addr) *Header {
	if version < 1 || version > 2 {
		version = 2
	}
	h := &Header{
		Version:           version,
		Command:           PROXY,
		TransportProtocol: UNSPEC,
	}
	switch sourceAddr := sourceAddr.(type) {
	case *net.TCPAddr:
		destAddr, ok := destAddr.(*net.TCPAddr)
		if !ok {
			break
		}
		if len(sourceAddr.IP.To4()) == net.IPv4len {
			h.TransportProtocol = TCPv4
		} else if len(sourceAddr.IP) == net.IPv6len {
			h.TransportProtocol = TCPv6
		} else {
			break
		}
		h.SourceAddress = sourceAddr.IP
		h.DestinationAddress = destAddr.IP
		h.SourcePort = uint16(sourceAddr.Port)
		h.DestinationPort = uint16(destAddr.Port)
	case *net.UDPAddr:
		destAddr, ok := destAddr.(*net.UDPAddr)
		if !ok {
			break
		}
		if len(sourceAddr.IP.To4()) == net.IPv4len {
			h.TransportProtocol = UDPv4
		} else if len(sourceAddr.IP) == net.IPv6len {
			h.TransportProtocol = UDPv6
		} else {
			break
		}
		h.SourceAddress = sourceAddr.IP
		h.DestinationAddress = destAddr.IP
		h.SourcePort = uint16(sourceAddr.Port)
		h.DestinationPort = uint16(destAddr.Port)
	case *net.UnixAddr:
		_, ok := destAddr.(*net.UnixAddr)
		if !ok {
			break
		}
		switch sourceAddr.Net {
		case "unix":
			h.TransportProtocol = UnixStream
		case "unixgram":
			h.TransportProtocol = UnixDatagram
		}
	}
	return h
}

// RemoteAddr returns the address of the remote endpoint of the connection.
func (header *Header) RemoteAddr() net.Addr {
	return &net.TCPAddr{
		IP:   header.SourceAddress,
		Port: int(header.SourcePort),
	}
}

// LocalAddr returns the address of the local endpoint of the connection.
func (header *Header) LocalAddr() net.Addr {
	return &net.TCPAddr{
		IP:   header.DestinationAddress,
		Port: int(header.DestinationPort),
	}
}

// EqualTo returns true if headers are equivalent, false otherwise.
// Deprecated: use EqualsTo instead. This method will eventually be removed.
func (header *Header) EqualTo(otherHeader *Header) bool {
	return header.EqualsTo(otherHeader)
}

// EqualsTo returns true if headers are equivalent, false otherwise.
func (header *Header) EqualsTo(otherHeader *Header) bool {
	if otherHeader == nil {
		return false
	}
	if header.Command.IsLocal() {
		return true
	}
	// TLVs only exist for version 2
	if header.Version == 0x02 && !bytes.Equal(header.rawTLVs, otherHeader.rawTLVs) {
		return false
	}
	return header.Version == otherHeader.Version &&
		header.TransportProtocol == otherHeader.TransportProtocol &&
		header.SourceAddress.String() == otherHeader.SourceAddress.String() &&
		header.DestinationAddress.String() == otherHeader.DestinationAddress.String() &&
		header.SourcePort == otherHeader.SourcePort &&
		header.DestinationPort == otherHeader.DestinationPort
}

// WriteTo renders a proxy protocol header in a format and writes it to an io.Writer.
func (header *Header) WriteTo(w io.Writer) (int64, error) {
	buf, err := header.Format()
	if err != nil {
		return 0, err
	}

	return bytes.NewBuffer(buf).WriteTo(w)
}

// Format renders a proxy protocol header in a format to write over the wire.
func (header *Header) Format() ([]byte, error) {
	switch header.Version {
	case 1:
		return header.formatVersion1()
	case 2:
		return header.formatVersion2()
	default:
		return nil, ErrUnknownProxyProtocolVersion
	}
}

// TLVs returns the TLVs stored into this header, if they exist.  TLVs are optional for v2 of the protocol.
func (header *Header) TLVs() ([]TLV, error) {
	return SplitTLVs(header.rawTLVs)
}

// SetTLVs sets the TLVs stored in this header. This method replaces any
// previous TLV.
func (header *Header) SetTLVs(tlvs []TLV) error {
	raw, err := JoinTLVs(tlvs)
	if err != nil {
		return err
	}
	header.rawTLVs = raw
	return nil
}

// Read identifies the proxy protocol version and reads the remaining of
// the header, accordingly.
//
// If proxy protocol header signature is not present, the reader buffer remains untouched
// and is safe for reading outside of this code.
//
// If proxy protocol header signature is present but an error is raised while processing
// the remaining header, assume the reader buffer to be in a corrupt state.
// Also, this operation will block until enough bytes are available for peeking.
func Read(reader *bufio.Reader) (*Header, error) {
	// In order to improve speed for small non-PROXYed packets, take a peek at the first byte alone.
	b1, err := reader.Peek(1)
	if err != nil {
		if err == io.EOF {
			return nil, ErrNoProxyProtocol
		}
		return nil, err
	}

	if bytes.Equal(b1[:1], SIGV1[:1]) || bytes.Equal(b1[:1], SIGV2[:1]) {
		signature, err := reader.Peek(5)
		if err != nil {
			if err == io.EOF {
				return nil, ErrNoProxyProtocol
			}
			return nil, err
		}
		if bytes.Equal(signature[:5], SIGV1) {
			return parseVersion1(reader)
		}

		signature, err = reader.Peek(12)
		if err != nil {
			if err == io.EOF {
				return nil, ErrNoProxyProtocol
			}
			return nil, err
		}
		if bytes.Equal(signature[:12], SIGV2) {
			return parseVersion2(reader)
		}
	}

	return nil, ErrNoProxyProtocol
}

// ReadTimeout acts as Read but takes a timeout. If that timeout is reached, it's assumed
// there's no proxy protocol header.
func ReadTimeout(reader *bufio.Reader, timeout time.Duration) (*Header, error) {
	type header struct {
		h *Header
		e error
	}
	read := make(chan *header, 1)

	go func() {
		h := &header{}
		h.h, h.e = Read(reader)
		read <- h
	}()

	timer := time.NewTimer(timeout)
	select {
	case result := <-read:
		timer.Stop()
		return result.h, result.e
	case <-timer.C:
		return nil, ErrNoProxyProtocol
	}
}
