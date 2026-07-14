package session

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/HenZenKuriRIP/XrayR4u/proxy/anytls/lib/padding"
	"github.com/HenZenKuriRIP/XrayR4u/proxy/anytls/lib/util"
	"github.com/sagernet/sing/common/atomic"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/logger"
)

type Session struct {
	conn     net.Conn
	connLock sync.Mutex

	streams    map[uint32]*Stream
	streamId   atomic.Uint32
	streamLock sync.RWMutex

	dieOnce sync.Once
	die     chan struct{}
	dieHook func()

	// synDone cancels the SYN watchdog timer. synPending counts outbound SYNs
	// for which SYNACK has not yet been received. Both are protected by
	// synDoneLock. The watchdog is armed on the 0→1 transition of synPending
	// and disarmed on the 1→0 transition, preventing both premature session
	// teardown under SYN bursts and silent leak of un-ACK'd SYNs.
	synDone     func()
	synDoneLock sync.Mutex
	synPending  int32

	// pool
	seq       uint64
	idleSince time.Time
	padding   *atomic.TypedValue[*padding.PaddingFactory]
	logger    logger.Logger

	peerVersion byte

	// client
	isClient    bool
	sendPadding bool
	buffering   bool
	buffer      []byte
	pktCounter  atomic.Uint32

	// server
	onNewStream func(stream *Stream)
}

func NewClientSession(conn net.Conn, _padding *atomic.TypedValue[*padding.PaddingFactory], logger logger.Logger) *Session {
	s := &Session{
		conn:        conn,
		isClient:    true,
		sendPadding: true,
		padding:     _padding,
		logger:      logger,
	}
	s.die = make(chan struct{})
	s.streams = make(map[uint32]*Stream)
	return s
}

func NewServerSession(conn net.Conn, onNewStream func(stream *Stream), _padding *atomic.TypedValue[*padding.PaddingFactory], logger logger.Logger) *Session {
	s := &Session{
		conn:        conn,
		onNewStream: onNewStream,
		padding:     _padding,
		logger:      logger,
	}
	s.die = make(chan struct{})
	s.streams = make(map[uint32]*Stream)
	return s
}

func (s *Session) Run() {
	if !s.isClient {
		s.recvLoop()
		return
	}

	settings := util.StringMap{
		"v":           "2",
		"client":      util.Verison,
		"padding-md5": s.padding.Load().Md5,
	}
	f := newFrame(cmdSettings, 0)
	f.data = settings.ToBytes()
	s.buffering = true
	s.writeControlFrame(f)

	go s.recvLoop()
}

// IsClosed does a safe check to see if we have shutdown
func (s *Session) IsClosed() bool {
	select {
	case <-s.die:
		return true
	default:
		return false
	}
}

// Close is used to close the session and all streams.
func (s *Session) Close() error {
	var once bool
	s.dieOnce.Do(func() {
		close(s.die)
		once = true
	})
	if once {
		if s.dieHook != nil {
			s.dieHook()
			s.dieHook = nil
		}
		s.streamLock.Lock()
		for _, stream := range s.streams {
			stream.closeLocally()
		}
		s.streams = make(map[uint32]*Stream)
		s.streamLock.Unlock()
		return s.conn.Close()
	} else {
		return io.ErrClosedPipe
	}
}

// OpenStream is used to create a new stream for CLIENT
func (s *Session) OpenStream() (*Stream, error) {
	if s.IsClosed() {
		return nil, io.ErrClosedPipe
	}

	sid := s.streamId.Add(1)
	stream := newStream(sid, s)

	if sid >= 2 && s.peerVersion >= 2 {
		s.synDoneLock.Lock()
		s.synPending++
		if s.synPending == 1 {
			// First in-flight SYN: arm the watchdog. Subsequent OpenStream
			// calls while SYNs are outstanding do NOT reset the deadline —
			// the watchdog was started when the first SYN was sent and fires
			// only if the server stops responding.
			if s.synDone != nil {
				s.synDone() // defensive: cancel any stale watcher
			}
			s.synDone = util.NewDeadlineWatcher(synAckTimeout, func() {
				s.Close()
			})
		}
		s.synDoneLock.Unlock()
	}

	if _, err := s.writeControlFrame(newFrame(cmdSYN, sid)); err != nil {
		return nil, err
	}

	s.buffering = false // proxy Write it's SocksAddr to flush the buffer

	s.streamLock.Lock()
	defer s.streamLock.Unlock()
	select {
	case <-s.die:
		return nil, io.ErrClosedPipe
	default:
		s.streams[sid] = stream
		return stream, nil
	}
}

func (s *Session) recvLoop() error {
	// defer func() {
	// 	if r := recover(); r != nil {
	// 		logrus.Errorln("[BUG]", r, string(debug.Stack()))
	// 	}
	// }()
	defer s.Close()

	var receivedSettingsFromClient bool
	var hdr rawHeader

	for {
		if s.IsClosed() {
			return io.ErrClosedPipe
		}
		// read header first
		if _, err := io.ReadFull(s.conn, hdr[:]); err == nil {
			sid := hdr.StreamID()
			switch hdr.Cmd() {
			case cmdPSH:
				if hdr.Length() > 0 {
					buffer := buf.Get(int(hdr.Length()))
					if _, err := io.ReadFull(s.conn, buffer); err == nil {
						s.streamLock.RLock()
						stream, ok := s.streams[sid]
						s.streamLock.RUnlock()
						if ok {
							// Copy into a private heap slice before returning the pool
							// buffer. pipeW.Write is synchronous and the pipe copies data
							// into the reader's buffer before acknowledging, so buf.Put
							// after Write is technically safe today — but a defensive copy
							// decouples pool lifetime from pipe transfer, eliminating the
							// risk if the pipe ever adopts asynchronous buffering.
							data := make([]byte, len(buffer))
							copy(data, buffer)
							buf.Put(buffer)
							stream.pipeW.Write(data)
						} else {
							buf.Put(buffer)
						}
					} else {
						buf.Put(buffer)
						return err
					}
				}
			case cmdSYN: // should be server only
				if !s.isClient && !receivedSettingsFromClient {
					f := newFrame(cmdAlert, 0)
					f.data = []byte("client did not send its settings")
					s.writeControlFrame(f)
					return nil
				}
				s.streamLock.Lock()
				if _, ok := s.streams[sid]; !ok {
					stream := newStream(sid, s)
					s.streams[sid] = stream
					go func() {
						if s.onNewStream != nil {
							s.onNewStream(stream)
						} else {
							stream.Close()
						}
					}()
				}
				s.streamLock.Unlock()
			case cmdSYNACK: // should be client only
				s.synDoneLock.Lock()
				if s.synPending > 0 {
					s.synPending--
					if s.synPending == 0 && s.synDone != nil {
						// All outstanding SYNs acknowledged: disarm the watchdog.
						s.synDone()
						s.synDone = nil
					}
				}
				s.synDoneLock.Unlock()
				if hdr.Length() > 0 {
					buffer := buf.Get(int(hdr.Length()))
					if _, err := io.ReadFull(s.conn, buffer); err != nil {
						buf.Put(buffer)
						return err
					}
					// report error
					s.streamLock.RLock()
					stream, ok := s.streams[sid]
					s.streamLock.RUnlock()
					if ok {
						stream.closeWithError(fmt.Errorf("remote: %s", string(buffer)))
					}
					buf.Put(buffer)
				}
			case cmdFIN:
				s.streamLock.Lock()
				stream, ok := s.streams[sid]
				delete(s.streams, sid)
				s.streamLock.Unlock()
				if ok {
					stream.closeLocally()
				}
			case cmdWaste:
				if hdr.Length() > 0 {
					buffer := buf.Get(int(hdr.Length()))
					if _, err := io.ReadFull(s.conn, buffer); err != nil {
						buf.Put(buffer)
						return err
					}
					buf.Put(buffer)
				}
			case cmdSettings:
				if hdr.Length() > 0 {
					buffer := buf.Get(int(hdr.Length()))
					if _, err := io.ReadFull(s.conn, buffer); err != nil {
						buf.Put(buffer)
						return err
					}
					if !s.isClient {
						receivedSettingsFromClient = true
						m := util.StringMapFromBytes(buffer)
						paddingF := s.padding.Load()
						if m["padding-md5"] != paddingF.Md5 {
							f := newFrame(cmdUpdatePaddingScheme, 0)
							f.data = paddingF.RawScheme
							_, err = s.writeControlFrame(f)
							if err != nil {
								buf.Put(buffer)
								return err
							}
						}
						// check client's version
						if v, err := strconv.Atoi(m["v"]); err == nil && v >= 2 {
							s.peerVersion = byte(v)
							// send cmdServerSettings
							f := newFrame(cmdServerSettings, 0)
							f.data = util.StringMap{
								"v": "2",
							}.ToBytes()
							_, err = s.writeControlFrame(f)
							if err != nil {
								buf.Put(buffer)
								return err
							}
						}
					}
					buf.Put(buffer)
				}
			case cmdAlert:
				if hdr.Length() > 0 {
					buffer := buf.Get(int(hdr.Length()))
					if _, err := io.ReadFull(s.conn, buffer); err != nil {
						buf.Put(buffer)
						return err
					}
					if s.isClient {
						s.logger.Error("[Alert from server]", string(buffer))
					}
					buf.Put(buffer)
					return nil
				}
			case cmdUpdatePaddingScheme:
				if hdr.Length() > 0 {
					// `rawScheme` Do not use buffer to prevent subsequent misuse
					rawScheme := make([]byte, int(hdr.Length()))
					if _, err := io.ReadFull(s.conn, rawScheme); err != nil {
						return err
					}
					if s.isClient {
						if padding.UpdatePaddingScheme(rawScheme, s.padding) {
							s.logger.Debug(fmt.Sprintf("[Update padding succeed] %x\n", md5.Sum(rawScheme)))
						} else {
							s.logger.Warn(fmt.Sprintf("[Update padding failed] %x\n", md5.Sum(rawScheme)))
						}
					}
				}
			case cmdHeartRequest:
				if _, err := s.writeControlFrame(newFrame(cmdHeartResponse, sid)); err != nil {
					return err
				}
			case cmdHeartResponse:
				// Active keepalive checking is not implemented yet
				break
			case cmdServerSettings:
				if hdr.Length() > 0 {
					buffer := buf.Get(int(hdr.Length()))
					if _, err := io.ReadFull(s.conn, buffer); err != nil {
						buf.Put(buffer)
						return err
					}
					if s.isClient {
						// check server's version
						m := util.StringMapFromBytes(buffer)
						if v, err := strconv.Atoi(m["v"]); err == nil {
							s.peerVersion = byte(v)
						}
					}
					buf.Put(buffer)
				}
			default:
				// I don't know what command it is (can't have data)
			}
		} else {
			return err
		}
	}
}

func (s *Session) streamClosed(sid uint32) error {
	if s.IsClosed() {
		return io.ErrClosedPipe
	}
	_, err := s.writeControlFrame(newFrame(cmdFIN, sid))
	s.streamLock.Lock()
	delete(s.streams, sid)
	s.streamLock.Unlock()
	return err
}

func (s *Session) writeDataFrame(sid uint32, data []byte) (int, error) {
	dataLen := len(data)

	buffer := buf.NewSize(dataLen + headerOverHeadSize)
	buffer.WriteByte(cmdPSH)
	binary.BigEndian.PutUint32(buffer.Extend(4), sid)
	binary.BigEndian.PutUint16(buffer.Extend(2), uint16(dataLen))
	buffer.Write(data)
	_, err := s.writeConnDeadlined(buffer.Bytes(), dataFrameWriteTimeout)
	buffer.Release()
	if err != nil {
		s.Close()
		return 0, err
	}

	return dataLen, nil
}

const (
	// controlFrameWriteTimeout bounds each control-frame write under connLock.
	controlFrameWriteTimeout = 5 * time.Second
	// dataFrameWriteTimeout bounds each data-frame write under connLock. Data
	// frames carry user payload and may be much larger than control frames;
	// a generous 30-second deadline prevents a stalled remote peer from
	// blocking the shared connLock indefinitely while still accommodating
	// slow paths (high-latency links, TCP congestion).
	dataFrameWriteTimeout = 30 * time.Second
	// synAckTimeout is the maximum time the client waits for the server to
	// acknowledge at least one pending SYN. It is deliberately generous (10 s)
	// to tolerate bursts of concurrent stream opens against a loaded server.
	synAckTimeout = 10 * time.Second
)

// writeControlFrame serializes a framed control message. The 5-second write
// deadline is set atomically under connLock via writeConnDeadlined, preventing
// the race where one goroutine clears another goroutine's deadline.
func (s *Session) writeControlFrame(frame frame) (int, error) {
	dataLen := len(frame.data)

	buffer := buf.NewSize(dataLen + headerOverHeadSize)
	buffer.WriteByte(frame.cmd)
	binary.BigEndian.PutUint32(buffer.Extend(4), frame.sid)
	binary.BigEndian.PutUint16(buffer.Extend(2), uint16(dataLen))
	buffer.Write(frame.data)

	_, err := s.writeConnDeadlined(buffer.Bytes(), controlFrameWriteTimeout)
	buffer.Release()
	if err != nil {
		s.Close()
		return 0, err
	}

	return dataLen, nil
}

// writeConnDeadlined acquires connLock, then sets a write deadline for the
// duration of the write so that the deadline and the write are atomic with
// respect to all other writers. This prevents the race where goroutine A sets
// a deadline, goroutine B clears it, and goroutine A then writes without one.
func (s *Session) writeConnDeadlined(b []byte, d time.Duration) (n int, err error) {
	s.connLock.Lock()
	defer s.connLock.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(d))
	n, err = s.lockedWrite(b)
	_ = s.conn.SetWriteDeadline(time.Time{})
	return
}

// writeConn acquires connLock and delegates to lockedWrite.
func (s *Session) writeConn(b []byte) (n int, err error) {
	s.connLock.Lock()
	defer s.connLock.Unlock()
	return s.lockedWrite(b)
}

// lockedWrite is the actual write implementation. connLock MUST be held by the caller.
func (s *Session) lockedWrite(b []byte) (n int, err error) {

	if s.buffering {
		s.buffer = slices.Concat(s.buffer, b)
		return len(b), nil
	} else if len(s.buffer) > 0 {
		b = slices.Concat(s.buffer, b)
		s.buffer = nil
	}

	// calculate & send padding
	if s.sendPadding {
		pkt := s.pktCounter.Add(1)
		paddingF := s.padding.Load()
		if pkt < paddingF.Stop {
			pktSizes := paddingF.GenerateRecordPayloadSizes(pkt)
			for _, l := range pktSizes {
				remainPayloadLen := len(b)
				if l == padding.CheckMark {
					if remainPayloadLen == 0 {
						break
					} else {
						continue
					}
				}
				if remainPayloadLen > l { // this packet is all payload
					_, err = s.conn.Write(b[:l])
					if err != nil {
						return 0, err
					}
					n += l
					b = b[l:]
				} else if remainPayloadLen > 0 { // this packet contains padding and the last part of payload
					paddingLen := l - remainPayloadLen - headerOverHeadSize
					if paddingLen > 0 {
						padding := make([]byte, headerOverHeadSize+paddingLen)
						padding[0] = cmdWaste
						binary.BigEndian.PutUint32(padding[1:5], 0)
						binary.BigEndian.PutUint16(padding[5:7], uint16(paddingLen))
						b = slices.Concat(b, padding)
					}
					_, err = s.conn.Write(b)
					if err != nil {
						return 0, err
					}
					n += remainPayloadLen
					b = nil
				} else { // this packet is all padding
					padding := make([]byte, headerOverHeadSize+l)
					padding[0] = cmdWaste
					binary.BigEndian.PutUint32(padding[1:5], 0)
					binary.BigEndian.PutUint16(padding[5:7], uint16(l))
					_, err = s.conn.Write(padding)
					if err != nil {
						return 0, err
					}
					b = nil
				}
			}
			// maybe still remain payload to write
			if len(b) == 0 {
				return
			} else {
				n2, err := s.conn.Write(b)
				return n + n2, err
			}
		} else {
			s.sendPadding = false
		}
	}

	return s.conn.Write(b)
}
