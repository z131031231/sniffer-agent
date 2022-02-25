package capture

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"github.com/google/gopacket/pcap"
	"github.com/google/gopacket/pcapgo"
	"golang.org/x/net/bpf"
	"math/rand"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	pp "github.com/pires/go-proxyproto"
	log "github.com/sirupsen/logrus"
	"sniffer-agent/communicator"
	"sniffer-agent/model"
	sd "sniffer-agent/session-dealer"
)

var (
	DeviceName  string //网卡名称
	snifferPort int    //被监听的端口
	// inParallel bool
)

func init() {
	flag.StringVar(&DeviceName, "interface", "eth0", "network device name. Default is eth0")
	flag.IntVar(&snifferPort, "port", 3306, "sniffer port. Default is 3306")
	// flag.BoolVar(&inParallel, "in_parallel", false, "if capture and deal package in parallel. Default is false")
}

// networkCard is network device
type networkCard struct {
	name       string   //网卡名称
	listenPort int      //被监听端口
	receiver   chan model.QueryPiece  //接收器
}


func NewNetworkCard() (nc *networkCard) {
	// init device
	return &networkCard{
		name:       DeviceName,
		listenPort: snifferPort,
		receiver:   make(chan model.QueryPiece, 100),
	}
}

func initEthernetHandlerFromPacpgo() (handler *pcapgo.EthernetHandle) {
	handler, err := pcapgo.NewEthernetHandle(DeviceName)
	if err != nil {
		panic(fmt.Sprintf("cannot open network interface %s <-- %s", DeviceName, err.Error()))
	}

	// set BPFFilter 创建数据包过滤器
	pcapBPF, err := pcap.CompileBPFFilter(
		layers.LinkTypeEthernet, 65535, fmt.Sprintf("tcp and (port %d)", snifferPort))
	if err != nil {
		panic(err.Error())
	}
	bpfIns := []bpf.RawInstruction{}
	for _, ins := range pcapBPF {
		bpfIn := bpf.RawInstruction{
			Op: ins.Code, //bpf虚拟指令的唯一标识
			Jt: ins.Jt,   //当条件为true时要跳过的指令数
			Jf: ins.Jf,   //当条件为false时要跳过的指令数
			K:  ins.K,    //常规参数，取决于Op
		}
		bpfIns = append(bpfIns, bpfIn)
	}

	err = handler.SetBPF(bpfIns)
	if err != nil {
		panic(err.Error())
	}

	_ = handler.SetCaptureLength(65536)

	return
}

// in online use, we found a strange bug: pcap cost 100% core CPU and memory increase along
/*func initEthernetHandlerFromPacp() (handler *pcap.Handle) {
	handler, err := pcap.OpenLive(DeviceName, 65536, false, pcap.BlockForever)
	if err != nil {
		panic(fmt.Sprintf("cannot open network interface %s <-- %s", DeviceName, err.Error()))
	}

	err = handler.SetBPFFilter(fmt.Sprintf("tcp and (port %d)", snifferPort))
	if err != nil {
		panic(err.Error())
	}

	return
}*/

func (nc *networkCard) Listen() (receiver chan model.QueryPiece) {
	nc.listenNormal()
	return nc.receiver
}

// Listen get a connection.
func (nc *networkCard) listenNormal() {
	go func() {
		aliveCounter := 0
		handler := initEthernetHandlerFromPacpgo()
		//handler := initEthernetHandlerFromPacp()
		for {
			var data []byte
			var ci gopacket.CaptureInfo  //线上捕获或从文件中读取的数据包的标准化信息。
			var err error

			// capture packets according to a certain probability
			capturePacketRate := communicator.GetTCPCapturePacketRate()
			if capturePacketRate <= 0 {
				time.Sleep(time.Second * 1)
				aliveCounter += 1
				if aliveCounter >= checkCount {
					aliveCounter = 0
					nc.receiver <- model.NewBaseQueryPiece(localIPAddr, nc.listenPort, capturePacketRate)
				}
				continue
			}

			data, ci, err = handler.ZeroCopyReadPacketData()
			if err != nil {
				log.Error(err.Error())
				time.Sleep(time.Second * 3)
				continue
			}

			packet := gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.NoCopy)
			//packet := gopacket.NewPacket(data, handler.LinkType(), gopacket.NoCopy)
			m := packet.Metadata()
			m.CaptureInfo = ci


			tcpPkt := packet.TransportLayer().(*layers.TCP)
			// send FIN tcp packet to avoid not complete session cannot be released
			// deal FIN packet
			// FIN 关闭连接
			if tcpPkt.FIN {
				nc.parseTCPPackage(packet, nil)
				continue
			}

			// deal auth packet
			if sd.IsAuthPacket(tcpPkt.Payload) {
				authHeader, _ := pp.Read(bufio.NewReader(bytes.NewReader(tcpPkt.Payload)))
				nc.parseTCPPackage(packet, authHeader)
				continue
			}

			if 0 < capturePacketRate && capturePacketRate < 1.0 {
				// fall into throw range
				rn := rand.Float64()
				if rn > capturePacketRate {
					continue
				}
			}

			aliveCounter = 0
			nc.parseTCPPackage(packet, nil)
		}
	}()

	return
}

func (nc *networkCard) parseTCPPackage(packet gopacket.Packet, authHeader *pp.Header) {


	var err error
	defer func() {
		if err != nil {
			log.Error("parse TCP package failed <-- %s", err.Error())
		}
	}()

	tcpPkt := packet.TransportLayer().(*layers.TCP)
	//SYN 建立连接
	//RST 连接重置
	if tcpPkt.SYN || tcpPkt.RST {
		return
	}

	ipLayer := packet.Layer(layers.LayerTypeIPv4)
	if ipLayer == nil {
		err = fmt.Errorf("no ip layer found in package")
		return
	}

	ipInfo, ok := ipLayer.(*layers.IPv4)
	if !ok {
		err = fmt.Errorf("parsed no ip address")
		return
	}

	srcIP := ipInfo.SrcIP.String()
	dstIP := ipInfo.DstIP.String()

	srcPort := int(tcpPkt.SrcPort)
	dstPort := int(tcpPkt.DstPort)
	if dstPort == nc.listenPort {
		// get client ip from proxy auth info
		var clientIP *string
		var clientPort int
		if authHeader != nil && authHeader.SourceAddress.String() != srcIP {
			clientIPContent := authHeader.SourceAddress.String()
			clientIP = &clientIPContent
			clientPort = int(authHeader.SourcePort)
			err = readToServerPackage(clientIP, clientPort, &srcIP, srcPort, tcpPkt, nc.receiver)
			if err != nil {
				return
			}
		}else {

			err = readToServerPackage(&srcIP, srcPort, &srcIP, srcPort, tcpPkt, nc.receiver)
			if err != nil {
				return
			}
		}


	} else if srcPort == nc.listenPort {
		// deal mysql client request

		err = readFromServerPackage(&dstIP, dstPort, tcpPkt)
		if err != nil {
			return
		}
	}

	return
}

func readFromServerPackage(
	srcIP *string, srcPort int, tcpPkt *layers.TCP) (err error) {
	defer func() {
		if err != nil {
			log.Error("read Mysql package send from mysql server to client failed <-- %s", err.Error())
		}
	}()

	if tcpPkt.FIN {
		sessionKey := spliceSessionKey(srcIP, srcPort)
		session := sessionPool[*sessionKey]
		if session != nil {
			session.Close()
			delete(sessionPool, *sessionKey)
		}
		return
	}

	tcpPayload := tcpPkt.Payload
	if (len(tcpPayload) < 1) {
		return
	}

	sessionKey := spliceSessionKey(srcIP, srcPort)
	session := sessionPool[*sessionKey]
	if session != nil {
		pkt := model.NewTCPPacket(tcpPayload, int64(tcpPkt.Ack), false)
		session.ReceiveTCPPacket(pkt)
	}

	return
}

func readToServerPackage(
	clientIP *string, clientPort int, srcIP *string, srcPort int, tcpPkt *layers.TCP,
	receiver chan model.QueryPiece) (err error) {
	defer func() {
		if err != nil {
			log.Error("read package send from client to mysql server failed <-- %s", err.Error())
		}
	}()

	// when client try close connection remove session from session pool
	if tcpPkt.FIN {

		sessionKey := spliceSessionKey(srcIP, srcPort)
		session := sessionPool[*sessionKey]
		if session != nil {
			session.Close()
			delete(sessionPool, *sessionKey)
		}
		log.Debugf("close connection from %s", *sessionKey)
		return
	}

	tcpPayload := tcpPkt.Payload
	if (len(tcpPayload) < 1) {
		return
	}

	sessionKey := spliceSessionKey(srcIP, srcPort)
	session := sessionPool[*sessionKey]
	if session == nil {
		session = sd.NewSession(sessionKey, clientIP, clientPort, srcIP, srcPort, localIPAddr, snifferPort, receiver)
		sessionPool[*sessionKey] = session
	}

	pkt := model.NewTCPPacket(tcpPayload, int64(tcpPkt.Seq), true)
	session.ReceiveTCPPacket(pkt)

	return
}
