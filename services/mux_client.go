package services

import (
	"crypto/tls"
	"log"
	"net"
	"proxy/utils"
	"time"

	"github.com/xtaci/smux"
)

type MuxClient struct {
	cfg MuxClientArgs
}

func NewMuxClient() Service {
	return &MuxClient{
		cfg: MuxClientArgs{},
	}
}

func (s *MuxClient) InitService() {

}

func (s *MuxClient) CheckArgs() {
	if *s.cfg.Parent != "" {
		log.Printf("use tls parent %s", *s.cfg.Parent)
	} else {
		log.Fatalf("parent required")
	}
	if *s.cfg.CertFile == "" || *s.cfg.KeyFile == "" {
		log.Fatalf("cert and key file required")
	}
	s.cfg.CertBytes, s.cfg.KeyBytes = utils.TlsBytes(*s.cfg.CertFile, *s.cfg.KeyFile)
}
func (s *MuxClient) StopService() {

}
func (s *MuxClient) Start(args interface{}) (err error) {
	s.cfg = args.(MuxClientArgs)
	s.CheckArgs()
	s.InitService()
	log.Printf("proxy on mux client mode")
	for {
		var _conn tls.Conn
		_conn, err = utils.TlsConnectHost(*s.cfg.Parent, *s.cfg.Timeout, s.cfg.CertBytes, s.cfg.KeyBytes)
		if err != nil {
			log.Printf("connection err: %s, retrying...", err)
			time.Sleep(time.Second * 3)
			continue
		}
		conn := net.Conn(&_conn)
		_, err = conn.Write(utils.BuildPacket(CONN_CLIENT, *s.cfg.Key))
		if err != nil {
			conn.Close()
			log.Printf("connection err: %s, retrying...", err)
			time.Sleep(time.Second * 3)
			continue
		}
		session, err := smux.Server(conn, nil)
		if err != nil {
			log.Printf("session err: %s, retrying...", err)
			conn.Close()
			time.Sleep(time.Second * 3)
			continue
		}
		for {
			stream, err := session.AcceptStream()
			if err != nil {
				log.Printf("accept stream err: %s, retrying...", err)
				session.Close()
				time.Sleep(time.Second * 3)
				break
			}
			go func() {
				var ID, clientLocalAddr, serverID string
				err = utils.ReadPacketData(stream, &ID, &clientLocalAddr, &serverID)
				if err != nil {
					log.Printf("read stream signal err: %s", err)
					stream.Close()
					return
				}
				log.Printf("signal revecived:%s %s %s", serverID, ID, clientLocalAddr)
				protocol := clientLocalAddr[:3]
				localAddr := clientLocalAddr[4:]
				if protocol == "udp" {
					s.ServeUDP(stream, localAddr, ID)
				} else {
					s.ServeConn(stream, localAddr, ID)
				}
			}()
		}
	}

}
func (s *MuxClient) Clean() {
	s.StopService()
}

func (s *MuxClient) ServeUDP(inConn *smux.Stream, localAddr, ID string) {

	for {
		srcAddr, body, err := utils.ReadUDPPacket(inConn)
		if err != nil {
			log.Printf("udp packet revecived fail, err: %s", err)
			log.Printf("connection %s released", ID)
			inConn.Close()
			break
		} else {
			//log.Printf("udp packet revecived:%s,%v", srcAddr, body)
			go s.processUDPPacket(inConn, srcAddr, localAddr, body)
		}

	}
	// }
}
func (s *MuxClient) processUDPPacket(inConn *smux.Stream, srcAddr, localAddr string, body []byte) {
	dstAddr, err := net.ResolveUDPAddr("udp", localAddr)
	if err != nil {
		log.Printf("can't resolve address: %s", err)
		inConn.Close()
		return
	}
	clientSrcAddr := &net.UDPAddr{IP: net.IPv4zero, Port: 0}
	conn, err := net.DialUDP("udp", clientSrcAddr, dstAddr)
	if err != nil {
		log.Printf("connect to udp %s fail,ERR:%s", dstAddr.String(), err)
		return
	}
	conn.SetDeadline(time.Now().Add(time.Millisecond * time.Duration(*s.cfg.Timeout)))
	_, err = conn.Write(body)
	if err != nil {
		log.Printf("send udp packet to %s fail,ERR:%s", dstAddr.String(), err)
		return
	}
	//log.Printf("send udp packet to %s success", dstAddr.String())
	buf := make([]byte, 1024)
	length, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		log.Printf("read udp response from %s fail ,ERR:%s", dstAddr.String(), err)
		return
	}
	respBody := buf[0:length]
	//log.Printf("revecived udp packet from %s , %v", dstAddr.String(), respBody)
	bs := utils.UDPPacket(srcAddr, respBody)
	_, err = (*inConn).Write(bs)
	if err != nil {
		log.Printf("send udp response fail ,ERR:%s", err)
		inConn.Close()
		return
	}
	//log.Printf("send udp response success ,from:%s ,%d ,%v", dstAddr.String(), len(bs), bs)
}
func (s *MuxClient) ServeConn(inConn *smux.Stream, localAddr, ID string) {
	var err error
	var outConn net.Conn
	i := 0
	for {
		i++
		outConn, err = utils.ConnectHost(localAddr, *s.cfg.Timeout)
		if err == nil || i == 3 {
			break
		} else {
			if i == 3 {
				log.Printf("connect to %s err: %s, retrying...", localAddr, err)
				time.Sleep(2 * time.Second)
				continue
			}
		}
	}
	if err != nil {
		inConn.Close()
		utils.CloseConn(&outConn)
		log.Printf("build connection error, err: %s", err)
		return
	}
	utils.IoBind(inConn, outConn, func(err interface{}) {
		log.Printf("conn %s released", ID)
	})
	log.Printf("conn %s created", ID)
}
