package relay

// #cgo LDFLAGS: -lsrt
// #include <srt/srt.h>
import "C"

import (
	"errors"
	"log"
	"strings"

	"github.com/haivision/srtgo"
)

var (
	InvalidStreamID     = errors.New("Invalid stream ID")
	InvalidMode         = errors.New("Invalid mode")
	StreamAlreadyExists = errors.New("Stream already exists")
	StreamNotExisting   = errors.New("Stream does not exist")
)

const (
	StreamIDSockOpt = 46

	PacketSize = 1456
)

const statsPeriodMs = 2

type Server interface {
	Handle(*srtgo.SrtSocket)
}

type ServerImpl struct {
	ps PubSub
}

func NewServer() Server {
	ps := NewPubSub()
	return &ServerImpl{ps}
}

type Mode uint8

const (
	_ Mode = iota
	Play
	Publish
)

func ParseStreamID(streamID string) (string, Mode, error) {
	split := strings.Split(streamID, "/")
	if len(split) != 2 {
		return "", 0, InvalidStreamID
	}
	name := split[0]
	modeStr := split[1]

	var mode Mode
	switch modeStr {
	case "play":
		mode = Play
	case "publish":
		mode = Publish
	default:
		return "", 0, InvalidMode
	}
	return name, mode, nil
}

func (s *ServerImpl) Handle(sock *srtgo.SrtSocket) {
	defer sock.Close()

	streamid, err := sock.GetSockOptString(C.SRTO_STREAMID)
	if err != nil {
		log.Println(err)
		return
	}

	name, mode, err := ParseStreamID(streamid)
	if err != nil {
		log.Println(err)
		return
	}

	switch mode {
	case Play:
		err = s.play(name, sock)
	case Publish:
		err = s.publish(name, sock)
	}
	if err != nil {
		log.Println(err)
	}
}

func (s *ServerImpl) play(name string, sock *srtgo.SrtSocket) error {
	sub, unsubscribe, err := s.ps.Subscribe(name)
	if err != nil {
		return err
	}
	defer unsubscribe()

	log.Println("Subscribe", name)

	dmx := Demuxer{}
	playing := false
	for {
		buf, ok := <-sub

		// Upstream closed, drop connection
		if !ok {
			return nil
		}

		// Find synchronization point
		if !playing {
			init, err := dmx.FindInit(buf)
			if err != nil {
				return err
			} else if init != nil {
				for i := range init {
					buf := init[i]
					sock.Write(buf, len(buf))
				}
				playing = true
			} else {
				continue
			}
		}

		// Write to socket
		sock.Write(buf, len(buf))
	}
}

func (s *ServerImpl) publish(name string, sock *srtgo.SrtSocket) error {
	pub, err := s.ps.Publish(name)
	if err != nil {
		return err
	}
	defer close(pub)

	log.Println("Publish", name)
	for {
		buf := make([]byte, PacketSize)
		n, err := sock.Read(buf, PacketSize)
		if err != nil {
			log.Println(err)
			return nil
		}
		// EOF
		if n == 0 {
			return nil
		}

		pub <- buf[:n]
	}
}
