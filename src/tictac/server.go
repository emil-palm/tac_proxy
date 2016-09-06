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
	"time"
	"io"

)

type Attachment struct {
	Color		string `json:"color"`
	Text		string `json:"text"`
	Author_name	string `json:"author_name"`
}

type Webhook struct {
	Username	string `json:"username"`
	Attachments []Attachment `json:"attachments"`
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
	for proxyname, _ := range viper.GetStringMap("proxies") {
		for _, element := range viper.GetStringSlice(fmt.Sprintf("proxies.%s.elements", proxyname)) {
			_, cidrnet, err := net.ParseCIDR(element)
			if err != nil {
				log.Print(fmt.Sprintf("Cannot parse cidr; %s", element))
			}
			if cidrnet.Contains(net.ParseIP(peer)) {
				proxy = proxyname
			}
		}
	}
	if proxy == "" {
		log.Print(fmt.Sprintf("Disconnecting %s (%s), cannot find a suitable proxy", peer_name, peer))
		s.conn.Close()
		return;
	}

	var proxy_object = viper.Sub(fmt.Sprintf("proxies.%s", proxy))

	log.Print(fmt.Sprintf("Connecting to upstream %s for proxy %s",fmt.Sprintf("%s:%s", proxy_object.GetString("upstream.address"), proxy_object.GetString("upstream.port")), proxy))

	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%s", proxy_object.GetString("upstream.address"), proxy_object.GetString("upstream.port")))
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
			// Proxy session
			ps := NewSession(conn)
			ps.key = []byte(proxy_object.GetString("upstream.key"))

			// Copy data from incomming to proxy packet
			ps.seq = p.seq
			ps.sessionId = p.sessionId

			pp := ps.genPacket(p.packetType, p.version)
			pp.data = p.data

			pp.cryptData(ps.key)

			pp.serialize(conn)

			pp,_ = ps.readPacket()

			p = s.genPacket(pp.packetType, p.version)
			p.data = pp.data
			p.cryptData(s.key)
			p.serialize(s.conn)
			if p.packetType == TAC_PLUS_AUTHEN {
				if viper.GetBool("mattermost.webhook.enable") {
					webhook := &Webhook{}
					webhook.Username = viper.GetString("mattermost.webhook.username")
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
				}
			}
		} else if err == io.EOF {
			break
		}
	}
	s.conn.Close()
	conn.Close()
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
