package ddp

import (
	"context"
	"fmt"
	"image/color"
	"log"
	"net"

	"wled-simulator/internal/state"
)

type Server struct {
	port        int
	state       *state.LEDState
	conn        *net.UDPConn
	ctx         context.Context
	cancel      context.CancelFunc
	lastPushSeq uint8
	ddpSeenPush bool
	verbose     bool
}

func NewServer(port int, s *state.LEDState) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		port:    port,
		state:   s,
		ctx:     ctx,
		cancel:  cancel,
		verbose: false, // Disable verbose logging by default
	}
}

// processPacket processes a validated DDP packet
func (s *Server) processPacket(header *DDPHeader, data []byte) error {
	headerSize := MinHeaderSize
	if header.HasTimecode {
		headerSize = MaxHeaderSize
	}

	payload := data[headerSize : headerSize+int(header.DataLength)]

	if s.verbose {
		typeStr := "undefined"
		switch header.DataType.Type {
		case TypeRGB:
			typeStr = "RGB"
		case TypeHSL:
			typeStr = "HSL"
		case TypeRGBW:
			typeStr = "RGBW"
		case TypeGrayscale:
			typeStr = "Grayscale"
		}

		customStr := ""
		if header.DataType.IsCustom {
			customStr = " (custom)"
		}

		log.Printf("[DDP] Processing packet: version=%d, seq=%d, type=%s%s (%d bits/element), device=%d, offset=%d, length=%d",
			header.Version, header.Sequence, typeStr, customStr, header.DataType.BitsPerElement,
			header.DeviceID, header.DataOffset, header.DataLength)
	}

	// Handle query packets
	if header.Query {
		if s.verbose {
			log.Printf("[DDP] Query packet received - not implemented")
		}
		return nil
	}

	// Process pixel data based on data type
	bytesPerPixel := 3 // Default RGB
	if header.DataType.Type == TypeRGBW {
		bytesPerPixel = 4
	}

	leds := s.state.LEDs()
	maxIndex := len(leds)
	startIndex := int(header.DataOffset) / bytesPerPixel

	// Build a batch of LED colors, then write to the pending buffer in one call
	colors := make([]color.RGBA, 0, len(payload)/bytesPerPixel)
	for i := 0; i+bytesPerPixel-1 < len(payload); i += bytesPerPixel {
		ledIndex := startIndex + len(colors)
		if ledIndex >= maxIndex {
			break
		}
		if bytesPerPixel == 4 {
			colors = append(colors, color.RGBA{
				R: payload[i],
				G: payload[i+1],
				B: payload[i+2],
				A: payload[i+3],
			})
		} else {
			a := uint8(255)
			if s.state.IsRGBW() {
				a = 0
			}
			colors = append(colors, color.RGBA{
				R: payload[i],
				G: payload[i+1],
				B: payload[i+2],
				A: a,
			})
		}
	}

	s.state.SetLEDRangePending(startIndex, colors)

	if s.verbose {
		log.Printf("[DDP] Buffered %d LEDs starting at index %d", len(colors), startIndex)
	}

	return nil
}

// Start begins listening for DDP packets
func (s *Server) Start() error {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	s.conn = conn

	// Start packet processing in a goroutine
	errChan := make(chan error, 1)
	go func() {
		defer conn.Close()
		buf := make([]byte, 1500)
		for {
			select {
			case <-s.ctx.Done():
				return
			default:
				n, remoteAddr, err := conn.ReadFromUDP(buf)
				if err != nil {
					if s.ctx.Err() != nil {
						return // Normal shutdown
					}
					log.Printf("[DDP] UDP read error: %v", err)
					continue
				}

				// Parse and validate header
				header, err := ParseHeader(buf[:n])
				if err != nil {
					s.state.ReportActivity(state.ActivityDDP, false)
					if s.verbose {
						log.Printf("[DDP] Invalid packet from %s: %v", remoteAddr, err)
					}
					continue
				}

				// Additional validation with windowed sequence check
				if err := ValidateHeader(header, s.lastPushSeq); err != nil {
					s.state.ReportActivity(state.ActivityDDP, false)
					if s.verbose {
						log.Printf("[DDP] Packet validation failed from %s: %v", remoteAddr, err)
					}
					continue
				}

				// Process the packet (buffer pixel data)
				if err := s.processPacket(header, buf[:n]); err != nil {
					s.state.ReportActivity(state.ActivityDDP, false)
					if s.verbose {
						log.Printf("[DDP] Packet processing failed from %s: %v", remoteAddr, err)
					}
					continue
				}

				// PUSH-aware rendering matching real WLED v16 behavior:
				// Only commit the back-buffer on PUSH, or if we've never seen a PUSH packet.
				push := header.Push
				s.ddpSeenPush = s.ddpSeenPush || push
				if !s.ddpSeenPush || push {
					s.state.CommitPending()
					s.state.SetLive()
					sn := header.Sequence & 0x0F
					if sn != 0 {
						s.lastPushSeq = sn
					}
				}

				s.state.ReportActivity(state.ActivityDDP, true)
			}
		}
	}()

	// Wait a moment for any immediate startup errors
	select {
	case err := <-errChan:
		return err
	default:
		return nil
	}
}

func (s *Server) Stop() error {
	s.cancel()
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

// SetVerbose enables or disables verbose logging
func (s *Server) SetVerbose(verbose bool) {
	s.verbose = verbose
}
