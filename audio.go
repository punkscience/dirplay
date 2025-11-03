package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dhowden/tag"
	"github.com/gopxl/beep"
	"github.com/gopxl/beep/flac"
	"github.com/gopxl/beep/mp3"
	"github.com/gopxl/beep/speaker"
	"github.com/gopxl/beep/vorbis"
	"github.com/gopxl/beep/wav"
)

// AudioPlayer manages audio playback
type AudioPlayer struct {
	streamer   beep.StreamSeekCloser
	ctrl       *beep.Ctrl
	format     beep.Format
	playing    bool
	file       *os.File
	duration   time.Duration
	currentPos time.Duration
	ctx        context.Context
	cancel     context.CancelFunc
	positionCh chan time.Duration
	artist     string
	title      string
}

// NewAudioPlayer creates a new audio player instance
func NewAudioPlayer() *AudioPlayer {
	ctx, cancel := context.WithCancel(context.Background())
	return &AudioPlayer{
		ctx:        ctx,
		cancel:     cancel,
		positionCh: make(chan time.Duration, 1),
	}
}

// LoadTrack loads an audio file for playback
func (ap *AudioPlayer) LoadTrack(filePath string) error {
	// Close previous track if any
	ap.Close()

	// Create a new context for this track
	ap.ctx, ap.cancel = context.WithCancel(context.Background())

	// Open the audio file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}

	ap.file = file

	// Read metadata tags
	tags, err := tag.ReadFrom(file)
	if err == nil {
		ap.artist = tags.Artist()
		ap.title = tags.Title()
	} else {
		// Fallback to filename if no tags
		ap.title = filepath.Base(filePath)
		ap.artist = "Unknown Artist"
	}

	// Reset file pointer for audio decoding
	if _, err := file.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek file: %w", err)
	}

	// Decode based on file extension
	ext := strings.ToLower(filepath.Ext(filePath))
	var streamer beep.StreamSeekCloser
	var format beep.Format

	switch ext {
	case ".mp3":
		streamer, format, err = mp3.Decode(file)
	case ".wav":
		streamer, format, err = wav.Decode(file)
	case ".flac":
		streamer, format, err = flac.Decode(file)
	case ".ogg":
		streamer, format, err = vorbis.Decode(file)
	default:
		return fmt.Errorf("unsupported audio format: %s", ext)
	}

	if err != nil {
		file.Close()
		return fmt.Errorf("failed to decode audio: %w", err)
	}

	ap.streamer = streamer
	ap.format = format

	// Calculate duration
	ap.duration = format.SampleRate.D(streamer.Len())
	ap.currentPos = 0

	// Initialize speaker if not already done
	// Buffer size increased from 100ms to 250ms for smoother playback
	if err := speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/2)); err != nil {
		// Speaker might already be initialized, which is fine
	}

	return nil
}

// Play starts or resumes playback
func (ap *AudioPlayer) Play() error {
	if ap.streamer == nil {
		return fmt.Errorf("no track loaded")
	}

	if ap.playing {
		return nil // Already playing
	}

	// Create control wrapper for pause/resume functionality
	ap.ctrl = &beep.Ctrl{
		Streamer: ap.streamer,
		Paused:   false,
	}

	// Start position tracking
	go ap.trackPosition()

	// Start playback
	speaker.Play(ap.ctrl)
	ap.playing = true

	return nil
}

// Pause pauses playback
func (ap *AudioPlayer) Pause() {
	if ap.ctrl != nil && ap.playing {
		speaker.Lock()
		ap.ctrl.Paused = true
		speaker.Unlock()
	}
}

// Resume resumes playback
func (ap *AudioPlayer) Resume() {
	if ap.ctrl != nil && ap.playing {
		speaker.Lock()
		ap.ctrl.Paused = false
		speaker.Unlock()
	}
}

// IsPaused returns true if playback is paused
func (ap *AudioPlayer) IsPaused() bool {
	if ap.ctrl == nil {
		return false
	}
	speaker.Lock()
	paused := ap.ctrl.Paused
	speaker.Unlock()
	return paused
}

// Stop stops playback
func (ap *AudioPlayer) Stop() {
	if ap.playing {
		speaker.Clear()
		ap.playing = false
		ap.currentPos = 0
		if ap.cancel != nil {
			ap.cancel() // Stop the goroutine
		}
	}
}

// Close closes the audio player and releases resources
func (ap *AudioPlayer) Close() {
	ap.Stop()

	if ap.cancel != nil {
		ap.cancel()
		ap.cancel = nil
	}

	if ap.streamer != nil {
		ap.streamer.Close()
		ap.streamer = nil
	}

	if ap.file != nil {
		ap.file.Close()
		ap.file = nil
	}

	ap.ctrl = nil
}

// GetDuration returns the total duration of the current track
func (ap *AudioPlayer) GetDuration() time.Duration {
	return ap.duration
}

// GetPosition returns the current playback position
func (ap *AudioPlayer) GetPosition() time.Duration {
	return ap.currentPos
}

// GetPositionChan returns a channel that receives position updates
func (ap *AudioPlayer) GetPositionChan() <-chan time.Duration {
	return ap.positionCh
}

// Seek seeks to a specific position in the track
func (ap *AudioPlayer) Seek(pos time.Duration) error {
	if ap.streamer == nil {
		return fmt.Errorf("no track loaded")
	}

	// Convert time to sample position
	samples := ap.format.SampleRate.N(pos)

	// Seek to position
	if err := ap.streamer.Seek(samples); err != nil {
		return fmt.Errorf("failed to seek: %w", err)
	}

	ap.currentPos = pos
	return nil
}

// trackPosition tracks the current playback position
func (ap *AudioPlayer) trackPosition() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ap.ctx.Done():
			return
		case <-ticker.C:
			if ap.playing && ap.ctrl != nil && !ap.IsPaused() {
				// Update position based on time elapsed
				ap.currentPos += 100 * time.Millisecond

				// Don't exceed duration
				if ap.currentPos > ap.duration {
					ap.currentPos = ap.duration
				}

				// Send position update
				select {
				case ap.positionCh <- ap.currentPos:
				default:
					// Channel full, skip this update
				}

				// Check if track finished
				if ap.currentPos >= ap.duration {
					ap.playing = false
					return
				}
			}
		}
	}
}

// IsPlaying returns true if audio is currently playing
func (ap *AudioPlayer) IsPlaying() bool {
	return ap.playing && !ap.IsPaused()
}

// GetArtist returns the artist of the current track
func (ap *AudioPlayer) GetArtist() string {
	return ap.artist
}

// GetTitle returns the title of the current track
func (ap *AudioPlayer) GetTitle() string {
	return ap.title
}
