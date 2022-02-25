package session_dealer

import "sniffer-agent/model"

type ConnSession interface {
	ReceiveTCPPacket(*model.TCPPacket)
	Close()
}
