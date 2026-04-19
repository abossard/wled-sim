package recorder

import (
	"fmt"
	"image"
	"image/color/palette"
	"image/gif"
	"os"
	"os/exec"
	"sync"
	"time"

	"wled-simulator/internal/state"
)

// Options configures recording behavior.
type Options struct {
	Format   string // "gif", "mp4", "both"
	Duration int    // seconds
	FPS      int    // frames per second
	Rows     int
	Cols     int
	Wiring   string // "row" or "col"
}

// Recorder captures LED state frames and encodes them to GIF/MP4.
type Recorder struct {
	state      *state.LEDState
	opts       Options
	mu         sync.Mutex
	recording  bool
	stopCh     chan struct{}
	frames     []*image.Paletted
	delays     []int // centiseconds per frame for GIF
	OnComplete func(filename string, err error)
}

// New creates a Recorder.
func New(s *state.LEDState, opts Options) *Recorder {
	if opts.FPS <= 0 {
		opts.FPS = 10
	}
	if opts.Duration <= 0 {
		opts.Duration = 24
	}
	if opts.Format == "" {
		opts.Format = "both"
	}
	return &Recorder{
		state: s,
		opts:  opts,
	}
}

// UpdateOptions updates the recorder configuration (must not be called while recording).
func (r *Recorder) UpdateOptions(opts Options) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.recording {
		return
	}
	if opts.FPS > 0 {
		r.opts.FPS = opts.FPS
	}
	if opts.Duration > 0 {
		r.opts.Duration = opts.Duration
	}
	if opts.Format != "" {
		r.opts.Format = opts.Format
	}
	r.opts.Rows = opts.Rows
	r.opts.Cols = opts.Cols
	r.opts.Wiring = opts.Wiring
}

// Start begins recording frames. Returns an error if already recording.
func (r *Recorder) Start() error {
	r.mu.Lock()
	if r.recording {
		r.mu.Unlock()
		return fmt.Errorf("already recording")
	}
	r.recording = true
	r.stopCh = make(chan struct{})
	r.frames = nil
	r.delays = nil
	r.mu.Unlock()

	go r.captureLoop()
	return nil
}

// Stop ends recording and encodes the output file(s). Returns the primary filename.
func (r *Recorder) Stop() (string, error) {
	r.mu.Lock()
	if !r.recording {
		r.mu.Unlock()
		return "", fmt.Errorf("not recording")
	}
	close(r.stopCh)
	r.recording = false
	frames := r.frames
	delays := r.delays
	opts := r.opts
	r.frames = nil
	r.delays = nil
	r.mu.Unlock()

	return r.finalize(frames, delays, opts)
}

// IsRecording returns true if currently recording.
func (r *Recorder) IsRecording() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.recording
}

// finalize encodes captured frames into output files.
func (r *Recorder) finalize(frames []*image.Paletted, delays []int, opts Options) (string, error) {
	if len(frames) == 0 {
		return "", fmt.Errorf("no frames captured")
	}

	timestamp := time.Now().Format("20060102-150405")
	var primaryFile string

	// GIF output
	if opts.Format == "gif" || opts.Format == "both" {
		filename := fmt.Sprintf("recording-%s.gif", timestamp)
		if err := encodeGIF(filename, frames, delays); err != nil {
			return "", fmt.Errorf("GIF encode: %w", err)
		}
		primaryFile = filename
	}

	// MP4 output via ffmpeg
	if opts.Format == "mp4" || opts.Format == "both" {
		filename := fmt.Sprintf("recording-%s.mp4", timestamp)
		if err := encodeMP4(filename, frames, opts); err != nil {
			if primaryFile == "" {
				return "", fmt.Errorf("MP4 encode: %w", err)
			}
			// MP4 failed but GIF succeeded — non-fatal
			fmt.Printf("MP4 encoding failed (ffmpeg required): %v\n", err)
		} else {
			if primaryFile == "" {
				primaryFile = filename
			}
		}
	}

	return primaryFile, nil
}

const ledPixelSize = 8 // each LED rendered as 8x8 pixels

func (r *Recorder) captureLoop() {
	interval := time.Second / time.Duration(r.opts.FPS)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	maxFrames := r.opts.Duration * r.opts.FPS
	delayCentiseconds := int(100 / r.opts.FPS)

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			frame := r.captureFrame()
			r.mu.Lock()
			r.frames = append(r.frames, frame)
			r.delays = append(r.delays, delayCentiseconds)
			count := len(r.frames)
			r.mu.Unlock()

			if count >= maxFrames {
				// Auto-stop: finalize recording in background
				r.mu.Lock()
				if r.recording {
					close(r.stopCh)
					r.recording = false
					frames := r.frames
					delays := r.delays
					opts := r.opts
					r.frames = nil
					r.delays = nil
					r.mu.Unlock()

					go func() {
						filename, err := r.finalize(frames, delays, opts)
						if r.OnComplete != nil {
							r.OnComplete(filename, err)
						}
					}()
				} else {
					r.mu.Unlock()
				}
				return
			}
		}
	}
}

// captureFrame renders the current LED state to a paletted image.
func (r *Recorder) captureFrame() *image.Paletted {
	rows, cols := r.opts.Rows, r.opts.Cols
	width := cols * ledPixelSize
	height := rows * ledPixelSize

	img := image.NewPaletted(image.Rect(0, 0, width, height), palette.Plan9)
	leds := r.state.LEDs()
	isRGBW := r.state.IsRGBW()

	for ledIndex := range leds {
		row, col := ledIndexToGrid(ledIndex, rows, cols, r.opts.Wiring)
		ledColor := leds[ledIndex]
		if isRGBW {
			ledColor.A = 255
		}

		// Fill the LED's pixel block
		x0 := col * ledPixelSize
		y0 := row * ledPixelSize
		for dy := 0; dy < ledPixelSize; dy++ {
			for dx := 0; dx < ledPixelSize; dx++ {
				img.Set(x0+dx, y0+dy, ledColor)
			}
		}
	}
	return img
}

// ledIndexToGrid converts a linear LED index to grid position based on wiring.
func ledIndexToGrid(ledIndex, rows, cols int, wiring string) (row, col int) {
	if wiring == "col" {
		return ledIndex % rows, ledIndex / rows
	}
	return ledIndex / cols, ledIndex % cols
}

func encodeGIF(filename string, frames []*image.Paletted, delays []int) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	return gif.EncodeAll(f, &gif.GIF{
		Image: frames,
		Delay: delays,
	})
}

func encodeMP4(filename string, frames []*image.Paletted, opts Options) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not found in PATH")
	}

	width := opts.Cols * ledPixelSize
	height := opts.Rows * ledPixelSize

	// Use ffmpeg with raw video input piped via stdin
	cmd := exec.Command(ffmpegPath,
		"-y",
		"-f", "rawvideo",
		"-pixel_format", "rgb24",
		"-video_size", fmt.Sprintf("%dx%d", width, height),
		"-framerate", fmt.Sprintf("%d", opts.FPS),
		"-i", "pipe:0",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-crf", "23",
		filename,
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	// Write raw RGB frames
	buf := make([]byte, width*height*3)
	for _, frame := range frames {
		idx := 0
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				r, g, b, _ := frame.At(x, y).RGBA()
				buf[idx] = uint8(r >> 8)
				buf[idx+1] = uint8(g >> 8)
				buf[idx+2] = uint8(b >> 8)
				idx += 3
			}
		}
		if _, err := stdin.Write(buf); err != nil {
			stdin.Close()
			cmd.Wait()
			return err
		}
	}

	stdin.Close()
	return cmd.Wait()
}

// ListRecordings returns recording filenames in the given directory.
func ListRecordings(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) > 10 && name[:10] == "recording-" {
			files = append(files, name)
		}
	}
	return files, nil
}
