package tictac

import (
	"fmt"
	"net"
	"errors"
	"bufio"
	"github.com/spf13/viper"
	"net/http"
	"encoding/json"
	"bytes"
	"log/syslog"
	"log"
	"io"
	"time"
)

type Proxy struct {
	ProxyName string
	Cidr	  *net.IPNet
}

type Server struct {
	ProxyList []*Proxy;
	SpamFilter map[string]int64
}

var _s *Server
func init() {
	_s = new(Server)
	_s.SpamFilter = make(map[string]int64)
}

func GetServer() *Server {
	return _s
}

type Attachment struct {
	Color		string `json:"color"`
	Text		string `json:"text"`
	Author_name	string `json:"author_name"`
}

type Webhook struct {
	Username	string `json:"username"`
	Attachments 	[]Attachment `json:"attachments"`
	IconUrl		string `json:"icon_url"`
}

// session represents the state of a single tacacs session which is identified
// by the session_id field in the tacacs common header.
type session struct {
	conn      net.Conn
	seq       uint8
	sessionId uint32
	key       []byte
}

func NewSession(conn net.Conn) *session {
	return &session{
		conn: conn,
	}
}



func (s *session) Handle(logger *syslog.Writer) {
	peer, _, _ := net.SplitHostPort(s.conn.RemoteAddr().String())
	var peer_name = peer
	names, err := net.LookupAddr(peer)
	if err == nil {
		peer_name = names[0]
	}
	log.Print(fmt.Sprintf("New connection from %s (%s)", peer_name, peer))

	var proxy = "";

	// Validate remote address to find a proxy to use
	for  _, _proxy := range GetServer().ProxyList {

		if _proxy.Cidr.Contains(net.ParseIP(peer)) {
			proxy = _proxy.ProxyName
			break
		}
	}
	if proxy == "" {
		log.Print(fmt.Sprintf("Disconnecting %s (%s), cannot find a suitable proxy", peer_name, peer))
		s.conn.Close()
		return;
	}

	var proxy_object = viper.Sub(fmt.Sprintf("proxies.%s", proxy))



	dialer := new(net.Dialer)
	if sourceip := proxy_object.GetString("sourceip"); sourceip != "" {
		// Create the Dialer.

		dialer.LocalAddr = &net.TCPAddr{
			IP: net.ParseIP(sourceip), 
		}

		log.Print(fmt.Sprintf("Connecting to upstream %s for proxy %s with sourceip: %s",fmt.Sprintf("%s:%s", proxy_object.GetString("upstream.address"), proxy_object.GetString("upstream.port")), proxy, sourceip))
	} else {
			log.Print(fmt.Sprintf("Connecting to upstream %s for proxy %s",fmt.Sprintf("%s:%s", proxy_object.GetString("upstream.address"), proxy_object.GetString("upstream.port")), proxy ))

	}

	conn, err := dialer.Dial("tcp", fmt.Sprintf("%s:%s", proxy_object.GetString("upstream.address"), proxy_object.GetString("upstream.port")))
	if err != nil {
		log.Print(fmt.Sprintf("Error connecting to upstream %s; %s", proxy, err))
		s.conn.Close()
		return;
	}

	s.key = []byte(proxy_object.GetString("key"))
	s.conn.SetReadDeadline(time.Now().Add(time.Second*30))

	for {
		r := bufio.NewReader(s.conn)
		_, err = r.Peek(12)
		if err == nil {
			p,_ := s.bReadPacket(r)

			if p.packetType == TAC_PLUS_AUTHEN {
				if p.seq == 1 && viper.GetBool("mattermost.webhook.enable") {
					go sendWebhook(peer, peer_name, proxy)
				}
			}


			// Proxy session
			ps := NewSession(conn)
			ps.key = []byte(proxy_object.GetString("upstream.key"))

			// Copy data from incomming to proxy packet
			ps.seq = p.seq
			ps.sessionId = p.sessionId

			pp := ps.genPacket(p.packetType, p.version)
			pp.flags = p.flags
			pp.data = p.data
			pp.seq = p.seq
			pp.sessionId = p.sessionId
			pp.cryptData(ps.key)

			pp.serialize(conn)

			pp,err = ps.readPacket()
			if err != nil {
				log.Printf("ACS is abit retarded and closed the TCP session, aborting")
				break
			}

			p = s.genPacket(pp.packetType, p.version)
			p.sessionId = pp.sessionId
			p.seq = pp.seq // TEsting to fix sequence....
			p.flags = pp.flags
			p.data = pp.data
			p.cryptData(s.key)
			p.serialize(s.conn)
		} else if err == io.EOF {
			break
		}
	}
	s.conn.Close()
	conn.Close()
}

func sendWebhook(peer string, peer_name string, proxy string) {
	timer, ok := GetServer().SpamFilter[peer]
	if !ok || time.Now().Unix() >= timer {
		webhook := &Webhook{}
		webhook.Username = viper.GetString("mattermost.webhook.username")
		webhook.IconUrl = viper.GetString("mattermost.webhook.iconurl") 
		attachment := Attachment{}
		attachment.Color = "#ff0000"
		attachment.Author_name = proxy
		attachment.Text = fmt.Sprintf("%s (%s) is connecting to a old tacacs", peer_name, peer)

		webhook.Attachments = []Attachment{attachment}
		jsonStr, err := json.Marshal(&webhook)
		if err != nil {
			fmt.Printf("Could not format json string\n")
			return
		}
		req, err := http.NewRequest("POST", viper.GetString("mattermost.webhook.url"), bytes.NewBuffer(jsonStr))
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{}
		_, err = client.Do(req)
		if err != nil {
			log.Printf("%s", err)
			log.Print(fmt.Sprintf("Failed to send webhook to mattermost"))
		}
		GetServer().SpamFilter[peer] = time.Now().Unix() + int64(60);
	}
}

func (s *session) bReadPacket(br *bufio.Reader) (*packet, error) {
	p := &packet{}
	if err := p.parse(br); err != nil {
		return nil, err
	}

	// Go ahead and increment the sequence when we receive a packet
	s.seq = p.seq + 1

	if s.sessionId == 0 {
		// New session, set the session ID
		s.sessionId = p.sessionId
	} else if s.sessionId != p.sessionId {
		return nil, errors.New(fmt.Sprintf("Invalid session id.  Got '%x', expected '%x,", p.sessionId, s.sessionId))
	}

	// Decrypt the data based on the key
	p.cryptData(s.key)

	return p, nil
}

func (s *session) readPacket() (*packet, error) {
	p := &packet{}
	if err := p.parse(s.conn); err != nil {
		return nil, err //fmt.Printf("session %s: %s", s.conn.RemoteAddr(), err)
	}

	// Go ahead and increment the sequence when we receive a packet
	s.seq = p.seq + 1

	if s.sessionId == 0 {
		// New session, set the session ID
		s.sessionId = p.sessionId
	} else if s.sessionId != p.sessionId {
		return nil, errors.New(fmt.Sprintf("Invalid session id.  Got '%x', expected '%x,", p.sessionId, s.sessionId))
	}

	// Decrypt the data based on the key
	p.cryptData(s.key)

	return p, nil
}

func (s *session) genPacket(packetType uint8, ver packetVer) *packet {
	p := &packet{
		packetType: packetType,
		version:    ver,
		seq:        s.seq,
		sessionId:  s.sessionId,
	}
	return p
}
