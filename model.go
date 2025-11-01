package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PlayerModel represents the state of the music player TUI
type PlayerModel struct {
	playlist      []string
	currentIndex  int
	player        *AudioPlayer
	playing       bool
	paused        bool
	position      time.Duration
	duration      time.Duration
	width         int
	height        int
	err           error
	artist        string
	title         string
}

// Messages for the TUI
type tickMsg time.Time
type positionMsg time.Duration
type trackEndedMsg struct{}
type playErrorMsg error
type trackLoadedMsg struct {
	duration time.Duration
	artist   string
	title    string
}

// NewPlayerModel creates a new player model
func NewPlayerModel(playlist []string) *PlayerModel {
	return &PlayerModel{
		playlist:     playlist,
		currentIndex: 0,
		player:       NewAudioPlayer(),
	}
}

// Init initializes the model
func (m *PlayerModel) Init() tea.Cmd {
	// Start the first track
	return tea.Batch(
		m.loadCurrentTrack(),
		m.tickCmd(),
	)
}

// Update handles messages and updates the model
func (m *PlayerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.player.Close()
			return m, tea.Quit

		case " ":
			// Toggle pause/play
			if m.playing {
				if m.paused {
					m.player.Resume()
					m.paused = false
				} else {
					m.player.Pause()
					m.paused = true
				}
			}

		case "left":
			// Previous track
			m.player.Stop()
			m.currentIndex--
			if m.currentIndex < 0 {
				m.currentIndex = len(m.playlist) - 1 // Loop to last track
			}
			return m, m.loadCurrentTrack()

		case "right":
			// Next track
			m.player.Stop()
			m.currentIndex++
			if m.currentIndex >= len(m.playlist) {
				m.currentIndex = 0 // Loop back to first track
			}
			return m, m.loadCurrentTrack()
		}

	case tickMsg:
		// Get current position from player
		m.position = m.player.GetPosition()

		// Check if track ended
		if m.playing && m.position >= m.duration && m.duration > 0 {
			return m, func() tea.Msg {
				return trackEndedMsg{}
			}
		}

		return m, m.tickCmd()

	case trackEndedMsg:
		m.currentIndex++
		if m.currentIndex >= len(m.playlist) {
			m.currentIndex = 0 // Loop back to the first track
		}
		return m, m.loadCurrentTrack()

	case trackLoadedMsg:
		m.playing = true
		m.paused = false
		m.position = 0
		m.duration = msg.duration
		m.artist = msg.artist
		m.title = msg.title
		return m, nil

	case playErrorMsg:
		m.err = error(msg)
		return m, nil

	case positionMsg:
		m.position = time.Duration(msg)
	}

	return m, nil
}

// View renders the TUI
func (m *PlayerModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\nPress 'q' or 'esc' to quit", m.err)
	}

	if len(m.playlist) == 0 {
		return "No tracks in playlist\nPress 'q' or 'esc' to quit"
	}

	// Determine track display
	var trackDisplay string
	if m.title != "" && m.artist != "" {
		trackDisplay = fmt.Sprintf("%s - %s", m.artist, m.title)
	} else if m.title != "" {
		trackDisplay = m.title
	} else {
		// Fallback to filename
		trackDisplay = filepath.Base(m.playlist[m.currentIndex])
		if ext := filepath.Ext(trackDisplay); ext != "" {
			trackDisplay = strings.TrimSuffix(trackDisplay, ext)
		}
	}

	// Create styles
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#04B575")).
		MarginBottom(1)

	trackStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FAFAFA")).
		MarginBottom(1)

	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#626262")).
		MarginBottom(1)

	progressStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#04B575"))

	controlsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#626262")).
		MarginTop(2)

	// Build the UI
	var content strings.Builder
	
	// Title
	content.WriteString(titleStyle.Render("♪ dirplay"))
	content.WriteString("\n\n")

	// Current track
	content.WriteString(trackStyle.Render(fmt.Sprintf("Playing: %s", trackDisplay)))
	content.WriteString("\n")

	// Track info
	trackInfo := fmt.Sprintf("Track %d of %d", m.currentIndex+1, len(m.playlist))
	content.WriteString(statusStyle.Render(trackInfo))
	content.WriteString("\n")

	// Status
	status := "■ Stopped"
	if m.playing {
		if m.paused {
			status = "⏸ Paused"
		} else {
			status = "▶ Playing"
		}
	}
	content.WriteString(statusStyle.Render(status))
	content.WriteString("\n\n")

	// Progress bar
	progressBar := m.renderProgressBar(40)
	content.WriteString(progressStyle.Render(progressBar))
	content.WriteString("\n")

	// Time display
	posStr := formatDuration(m.position)
	durStr := formatDuration(m.duration)
	timeDisplay := fmt.Sprintf("%s / %s", posStr, durStr)
	content.WriteString(statusStyle.Render(timeDisplay))
	content.WriteString("\n")

	// Controls
	controls := "Controls: [←] Previous  [→] Next  [SPACE] Pause/Play  [ESC] Quit"
	content.WriteString(controlsStyle.Render(controls))

	return content.String()
}

// renderProgressBar renders a progress bar
func (m *PlayerModel) renderProgressBar(width int) string {
	if m.duration == 0 {
		return strings.Repeat("─", width)
	}

	progress := float64(m.position) / float64(m.duration)
	filled := int(progress * float64(width))
	
	if filled > width {
		filled = width
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("─", width-filled)
	return fmt.Sprintf("[%s]", bar)
}

// tickCmd returns a command to send tick messages
func (m *PlayerModel) tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// loadCurrentTrack loads and plays the current track
func (m *PlayerModel) loadCurrentTrack() tea.Cmd {
	return func() tea.Msg {
		if m.currentIndex >= len(m.playlist) {
			return nil
		}

		track := m.playlist[m.currentIndex]
		
		// Load the track
		if err := m.player.LoadTrack(track); err != nil {
			return playErrorMsg(fmt.Errorf("failed to load track: %w", err))
		}

		// Start playing
		if err := m.player.Play(); err != nil {
			return playErrorMsg(fmt.Errorf("failed to play track: %w", err))
		}

		return trackLoadedMsg{
			duration: m.player.GetDuration(),
			artist:   m.player.GetArtist(),
			title:    m.player.GetTitle(),
		}
	}
}

// formatDuration formats a time.Duration as mm:ss
func formatDuration(d time.Duration) string {
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}