package bufferless

import (
	"errors"
	"io"
)

var (

	// ErrNoTerminal indicates that no suitable terminal could be found.
	// This may result from attempting to run on a platform where there
	// is no support for either termios or console I/O (such as nacl),
	// or from running in an environment where there is no access to
	// a suitable console/terminal device.  (For example, running on
	// without a controlling TTY or with no /dev/tty on POSIX platforms.)
	ErrNoTerminal = errors.New("no suitable terminal available")
)

func NewTerminal(tcmode TruecolorMode, vtmode VtMode) (Terminal, error) {
	t, err := NewWindowsTerminal(tcmode, vtmode)

	if t == nil {

	}

	return t, err
}

type Terminal interface {
	WriteString(s string) error
	WriteRunes(runes []rune) (n int, err error)
	SetStyle(s Style)
	// ResetStyle?
	SetTitle(title string)
	ResetTitle()
	EnableAltMode()
	DisableAltMode()
	ClearScreen(clearScrollbackbuffer bool)
	ShowCursor()
	HideCursor()
	CursorPos() (int, int)
	SetCursorPos(x, y int)
	MoveCursor(x, y int)
	ResetCursorPos()
	SetCursorStyle(cs CursorStyle, cc Color)
	//Reset Cursor Style ?
	Size() (int, int)
	SetSize(int, int)
	HasMouse() bool
	EnableMouse()
	DisableMouse()
	EnablePaste()
	DisablePaste()
	Colors() int
	CharacterSet() string
	CanDisplay(r rune) bool

	HasKey(Key) bool
	Beep() error
	SetClipboard([]byte)
	GetClipboard()

	//set IO modes (raw vs cooked)
	GetModes() TerminalMode
	SetModes(mode TerminalMode)

	//start/stop event loop
	EventChan() <-chan Event

	Read(p []byte) (n int, err error)
	Write(p []byte) (n int, err error)
	Close() error

	io.ReadWriteCloser
}

type TruecolorMode int

const (
	TruecolorMode_system TruecolorMode = iota
	TruecolorMode_enable
	TruecolorMode_disable
)

type VtMode int

const (
	VtMode_system VtMode = iota
	VtMode_enable
	VtMode_disable
)

type AlternateMode int

const (
	AlternateMode_system AlternateMode = iota
	AlternateMode_enable
	AlternateMode_disable
)

// CursorStyle represents a given cursor style, which can include the shape and
// whether the cursor blinks or is solid.  Support for changing this is not universal.
type CursorStyle int

const (
	CursorStyleDefault = CursorStyle(iota) // The default
	CursorStyleBlinkingBlock
	CursorStyleSteadyBlock
	CursorStyleBlinkingUnderline
	CursorStyleSteadyUnderline
	CursorStyleBlinkingBar
	CursorStyleSteadyBar
)

type TerminalMode uint

const (
	TerminalModeWrapAtEOL TerminalMode = 1 << iota

	TerminalModeLineInput // as opposed to direct/raw input
	TerminalModeEchoInput
	// when enabled, CTRL C and the like
	// are being handled by the terminal instead of being send to the application
	// this is to enable cbreak/rare mode
	TerminalModeprocessControlCharacters

	TerminalModeRaw    TerminalMode = 0
	TerminalModeCooked TerminalMode = TerminalModeWrapAtEOL | TerminalModeLineInput |
		TerminalModeEchoInput | TerminalModeprocessControlCharacters
)
