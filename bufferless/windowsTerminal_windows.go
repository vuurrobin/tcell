//go:build windows

package bufferless

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	k32 = syscall.NewLazyDLL("kernel32.dll")
	u32 = syscall.NewLazyDLL("user32.dll")
)

// We have to bring in the kernel32 and user32 DLLs directly, so we can get
// access to some system calls that the core Go API lacks.
//
// Note that Windows appends some functions with W to indicate that wide
// characters (Unicode) are in use.  The documentation refers to them
// without this suffix, as the resolution is made via preprocessor.
var (
	procReadConsoleInput            = k32.NewProc("ReadConsoleInputW")
	procReadConsole                 = k32.NewProc("ReadConsoleW")
	procWaitForMultipleObjects      = k32.NewProc("WaitForMultipleObjects")
	procCreateEvent                 = k32.NewProc("CreateEventW")
	procSetEvent                    = k32.NewProc("SetEvent")
	procGetConsoleCursorInfo        = k32.NewProc("GetConsoleCursorInfo")
	procSetConsoleCursorInfo        = k32.NewProc("SetConsoleCursorInfo")
	procSetConsoleCursorPosition    = k32.NewProc("SetConsoleCursorPosition")
	procSetConsoleMode              = k32.NewProc("SetConsoleMode")
	procGetConsoleMode              = k32.NewProc("GetConsoleMode")
	procGetConsoleScreenBufferInfo  = k32.NewProc("GetConsoleScreenBufferInfo")
	procFillConsoleOutputAttribute  = k32.NewProc("FillConsoleOutputAttribute")
	procFillConsoleOutputCharacter  = k32.NewProc("FillConsoleOutputCharacterW")
	procSetConsoleWindowInfo        = k32.NewProc("SetConsoleWindowInfo")
	procSetConsoleScreenBufferSize  = k32.NewProc("SetConsoleScreenBufferSize")
	procSetConsoleTextAttribute     = k32.NewProc("SetConsoleTextAttribute")
	procGetLargestConsoleWindowSize = k32.NewProc("GetLargestConsoleWindowSize")
	procMessageBeep                 = u32.NewProc("MessageBeep")
)

var winLock sync.Mutex

var winPalette = []Color{
	ColorBlack,
	ColorMaroon,
	ColorGreen,
	ColorNavy,
	ColorOlive,
	ColorPurple,
	ColorTeal,
	ColorSilver,
	ColorGray,
	ColorRed,
	ColorLime,
	ColorBlue,
	ColorYellow,
	ColorFuchsia,
	ColorAqua,
	ColorWhite,
}

var winColors = map[Color]Color{
	ColorBlack:   ColorBlack,
	ColorMaroon:  ColorMaroon,
	ColorGreen:   ColorGreen,
	ColorNavy:    ColorNavy,
	ColorOlive:   ColorOlive,
	ColorPurple:  ColorPurple,
	ColorTeal:    ColorTeal,
	ColorSilver:  ColorSilver,
	ColorGray:    ColorGray,
	ColorRed:     ColorRed,
	ColorLime:    ColorLime,
	ColorBlue:    ColorBlue,
	ColorYellow:  ColorYellow,
	ColorFuchsia: ColorFuchsia,
	ColorAqua:    ColorAqua,
	ColorWhite:   ColorWhite,
}

var vgaColors = map[Color]uint16{
	ColorBlack:   0,
	ColorMaroon:  0x4,
	ColorGreen:   0x2,
	ColorNavy:    0x1,
	ColorOlive:   0x6,
	ColorPurple:  0x5,
	ColorTeal:    0x3,
	ColorSilver:  0x7,
	ColorGrey:    0x8,
	ColorRed:     0xc,
	ColorLime:    0xa,
	ColorBlue:    0x9,
	ColorYellow:  0xe,
	ColorFuchsia: 0xd,
	ColorAqua:    0xb,
	ColorWhite:   0xf,
}

const (
	// VT100/XTerm escapes understood by the console
	vtShowCursor              = "\x1b[?25h"
	vtHideCursor              = "\x1b[?25l"
	vtCursorPos               = "\x1b[%d;%dH" // Note that it is Y then X
	vtSgr0                    = "\x1b[0m"
	vtBold                    = "\x1b[1m"
	vtUnderline               = "\x1b[4m"
	vtBlink                   = "\x1b[5m" // Not sure if this is processed
	vtReverse                 = "\x1b[7m"
	vtSetFg                   = "\x1b[38;5;%dm"
	vtSetBg                   = "\x1b[48;5;%dm"
	vtSetFgRGB                = "\x1b[38;2;%d;%d;%dm" // RGB
	vtSetBgRGB                = "\x1b[48;2;%d;%d;%dm" // RGB
	vtCursorDefault           = "\x1b[0 q"
	vtCursorBlinkingBlock     = "\x1b[1 q"
	vtCursorSteadyBlock       = "\x1b[2 q"
	vtCursorBlinkingUnderline = "\x1b[3 q"
	vtCursorSteadyUnderline   = "\x1b[4 q"
	vtCursorBlinkingBar       = "\x1b[5 q"
	vtCursorSteadyBar         = "\x1b[6 q"
	vtDisableAm               = "\x1b[?7l"    // disable auto wrap mode
	vtEnableAm                = "\x1b[?7h"    // enable auto wrap mode
	vtEnterCA                 = "\x1b[?1049h" // removed "\x1b[22;0;0t", its not supported on windows
	vtExitCA                  = "\x1b[?1049l" // removed "\x1b[23;0;0t", its not supported on windows
	vtDoubleUnderline         = "\x1b[4:2m"
	vtCurlyUnderline          = "\x1b[4:3m"
	vtDottedUnderline         = "\x1b[4:4m"
	vtDashedUnderline         = "\x1b[4:5m"
	vtUnderColor              = "\x1b[58:5:%dm"
	vtUnderColorRGB           = "\x1b[58:2::%d:%d:%dm"
	vtUnderColorReset         = "\x1b[59m"
	vtEnterUrl                = "\x1b]8;%s;%s\x1b\\" // NB arg 1 is id, arg 2 is url
	vtExitUrl                 = "\x1b]8;;\x1b\\"
	vtCursorColorRGB          = "\x1b]12;#%02x%02x%02x\007"
	vtCursorColorReset        = "\x1b]112\007"
	vtSetTitle                = "\x1b]0;%s\x1b\\"
	// vtSaveTitle               = "\x1b[22;2t" // This doesn't seem to work
	// vtRestoreTitle            = "\x1b[23;2t" // This doesn't seem to work

	vtClearScreen           = "\x1b[2J"
	vtClearScrollbackbuffer = "\x1b[2J\x1b[3J"
	vtResetCursorpos        = "\x1b[H"

	vtMoveCursorUp    = "\x1b[%dA"
	vtMoveCursorDown  = "\x1b[%dB"
	vtMoveCursorRight = "\x1b[%dC"
	vtMoveCursorLeft  = "\x1b[%dD"

	vtReportCursorPosition = "\x1b[6n"
)

var vtCursorStyles = map[CursorStyle]string{
	CursorStyleDefault:           vtCursorDefault,
	CursorStyleBlinkingBlock:     vtCursorBlinkingBlock,
	CursorStyleSteadyBlock:       vtCursorSteadyBlock,
	CursorStyleBlinkingUnderline: vtCursorBlinkingUnderline,
	CursorStyleSteadyUnderline:   vtCursorSteadyUnderline,
	CursorStyleBlinkingBar:       vtCursorBlinkingBar,
	CursorStyleSteadyBar:         vtCursorSteadyBar,
}

// https://learn.microsoft.com/en-us/windows/console/high-level-console-modes
const (
	// Input modes
	modeCooked             uint32 = 0x0001 // ENABLE_PROCESSED_INPUT
	modeEnableLineInput    uint32 = 0x0002
	modeEnableEchoInput    uint32 = 0x0004
	modeResizeEn           uint32 = 0x0008 // ENABLE_WINDOW_INPUT
	modeMouseEn            uint32 = 0x0010
	modeEnableInsertMode   uint32 = 0x0020
	modeEnableQuickEdit    uint32 = 0x0040
	modeExtendFlg          uint32 = 0x0080 // ENABLE_EXTENDED_FLAGS
	modeEnableAutoPosition uint32 = 0x0100 // apparently undocumented
	modeVtInput            uint32 = 0x0200

	// Output modes
	modeCookedOut uint32 = 0x0001 // ENABLE_PROCESSED_OUTPUT
	modeWrapEOL   uint32 = 0x0002
	modeVtOutput  uint32 = 0x0004
	modeNoAutoNL  uint32 = 0x0008
	modeUnderline uint32 = 0x0010 // ENABLE_LVB_GRID_WORLDWIDE, needed for underlines
)

const (
	w32Infinite    = ^uintptr(0)
	w32WaitObject0 = uintptr(0)
)

const (
	keyEvent    uint16 = 1
	mouseEvent  uint16 = 2
	resizeEvent uint16 = 4
	menuEvent   uint16 = 8 // don't use
	focusEvent  uint16 = 16
)

const (
	// Constants per Microsoft.  We don't put the modifiers
	// here.
	vkCancel = 0x03
	vkBack   = 0x08 // Backspace
	vkTab    = 0x09
	vkClear  = 0x0c
	vkReturn = 0x0d
	vkPause  = 0x13
	vkEscape = 0x1b
	vkSpace  = 0x20
	vkPrior  = 0x21 // PgUp
	vkNext   = 0x22 // PgDn
	vkEnd    = 0x23
	vkHome   = 0x24
	vkLeft   = 0x25
	vkUp     = 0x26
	vkRight  = 0x27
	vkDown   = 0x28
	vkPrint  = 0x2a
	vkPrtScr = 0x2c
	vkInsert = 0x2d
	vkDelete = 0x2e
	vkHelp   = 0x2f
	vkF1     = 0x70
	vkF2     = 0x71
	vkF3     = 0x72
	vkF4     = 0x73
	vkF5     = 0x74
	vkF6     = 0x75
	vkF7     = 0x76
	vkF8     = 0x77
	vkF9     = 0x78
	vkF10    = 0x79
	vkF11    = 0x7a
	vkF12    = 0x7b
	vkF13    = 0x7c
	vkF14    = 0x7d
	vkF15    = 0x7e
	vkF16    = 0x7f
	vkF17    = 0x80
	vkF18    = 0x81
	vkF19    = 0x82
	vkF20    = 0x83
	vkF21    = 0x84
	vkF22    = 0x85
	vkF23    = 0x86
	vkF24    = 0x87
)

var vkKeys = map[uint16]Key{
	vkCancel: KeyCancel,
	vkBack:   KeyBackspace,
	vkTab:    KeyTab,
	vkClear:  KeyClear,
	vkPause:  KeyPause,
	vkPrint:  KeyPrint,
	vkPrtScr: KeyPrint,
	vkPrior:  KeyPgUp,
	vkNext:   KeyPgDn,
	vkReturn: KeyEnter,
	vkEnd:    KeyEnd,
	vkHome:   KeyHome,
	vkLeft:   KeyLeft,
	vkUp:     KeyUp,
	vkRight:  KeyRight,
	vkDown:   KeyDown,
	vkInsert: KeyInsert,
	vkDelete: KeyDelete,
	vkHelp:   KeyHelp,
	vkEscape: KeyEscape,
	vkSpace:  ' ',
	vkF1:     KeyF1,
	vkF2:     KeyF2,
	vkF3:     KeyF3,
	vkF4:     KeyF4,
	vkF5:     KeyF5,
	vkF6:     KeyF6,
	vkF7:     KeyF7,
	vkF8:     KeyF8,
	vkF9:     KeyF9,
	vkF10:    KeyF10,
	vkF11:    KeyF11,
	vkF12:    KeyF12,
	vkF13:    KeyF13,
	vkF14:    KeyF14,
	vkF15:    KeyF15,
	vkF16:    KeyF16,
	vkF17:    KeyF17,
	vkF18:    KeyF18,
	vkF19:    KeyF19,
	vkF20:    KeyF20,
	vkF21:    KeyF21,
	vkF22:    KeyF22,
	vkF23:    KeyF23,
	vkF24:    KeyF24,
}

const (
	mouseHWheeled uint32 = 0x8
	mouseVWheeled uint32 = 0x4
	// mouseDoubleClick uint32 = 0x2
	// mouseMoved       uint32 = 0x1
)

// NB: All Windows platforms are little endian.  We assume this
// never, ever change.  The following code is endian safe. and does
// not use unsafe pointers.
func getu32(v []byte) uint32 {
	return uint32(v[0]) + (uint32(v[1]) << 8) + (uint32(v[2]) << 16) + (uint32(v[3]) << 24)
}
func geti32(v []byte) int32 {
	return int32(getu32(v))
}
func getu16(v []byte) uint16 {
	return uint16(v[0]) + (uint16(v[1]) << 8)
}
func geti16(v []byte) int16 {
	return int16(getu16(v))
}

// Convert windows dwControlKeyState to modifier mask
func mod2mask(cks uint32) ModMask {
	mm := ModNone
	// Left or right control
	ctrl := (cks & (0x0008 | 0x0004)) != 0
	// Left or right alt
	alt := (cks & (0x0002 | 0x0001)) != 0
	// Filter out ctrl+alt (it means AltGr)
	if !(ctrl && alt) {
		if ctrl {
			mm |= ModCtrl
		}
		if alt {
			mm |= ModAlt
		}
	}
	// Any shift
	if (cks & 0x0010) != 0 {
		mm |= ModShift
	}
	return mm
}

func mrec2btns(mbtns, flags uint32) ButtonMask {
	btns := ButtonNone
	if mbtns&0x1 != 0 {
		btns |= Button1
	}
	if mbtns&0x2 != 0 {
		btns |= Button2
	}
	if mbtns&0x4 != 0 {
		btns |= Button3
	}
	if mbtns&0x8 != 0 {
		btns |= Button4
	}
	if mbtns&0x10 != 0 {
		btns |= Button5
	}
	if mbtns&0x20 != 0 {
		btns |= Button6
	}
	if mbtns&0x40 != 0 {
		btns |= Button7
	}
	if mbtns&0x80 != 0 {
		btns |= Button8
	}

	if flags&mouseVWheeled != 0 {
		if mbtns&0x80000000 == 0 {
			btns |= WheelUp
		} else {
			btns |= WheelDown
		}
	}
	if flags&mouseHWheeled != 0 {
		if mbtns&0x80000000 == 0 {
			btns |= WheelRight
		} else {
			btns |= WheelLeft
		}
	}
	return btns
}

type inputRecord struct {
	typ  uint16
	_    uint16
	data [16]byte
}

type coord struct {
	x int16
	y int16
}

func (c coord) uintptr() uintptr {
	// little endian, put x first
	return uintptr(c.x) | (uintptr(c.y) << 16)
}

type rect struct {
	left   int16
	top    int16
	right  int16
	bottom int16
}

type consoleInfo struct {
	size  coord
	pos   coord
	attrs uint16
	win   rect
	maxsz coord
}

type cursorInfo struct {
	size    uint32
	visible uint32
}

type windowsTerminal struct {
	in         syscall.Handle
	out        syscall.Handle
	cancelflag syscall.Handle

	oscreen consoleInfo
	ocursor cursorInfo

	origInmode, origOutmode uint32
	inmode, outmode         uint32
	termMode                TerminalMode

	curx, cury    int
	width, height int
	style         Style

	eventChan chan Event
	// runEventLoop chan struct{}
	quit chan struct{}

	vten      bool
	truecolor bool
}

var _ Terminal = (*windowsTerminal)(nil)
var _ io.ReadWriteCloser = (*windowsTerminal)(nil)

func NewWindowsTerminal(tcmode TruecolorMode, vtmode VtMode) (Terminal, error) {
	wt := windowsTerminal{}

	in, err := syscall.Open("CONIN$", syscall.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	wt.in = in
	out, err := syscall.Open("CONOUT$", syscall.O_RDWR, 0)
	if err != nil {
		_ = syscall.Close(wt.in)
		return nil, err
	}
	wt.out = out

	wt.curx = -1
	wt.cury = -1
	wt.style = StyleDefault
	wt.getOutMode(&wt.outmode)
	wt.getInMode(&wt.inmode)
	wt.origOutmode = wt.outmode
	wt.origInmode = wt.inmode

	wt.getConsoleInfo(&wt.oscreen)
	wt.getCursorInfo(&wt.ocursor)

	wt.retrieveSize()

	// ConEmu handling of colors and scrolling when in VT output mode is extremely poor.
	// The color palette will scroll even though characters do not, when
	// emitting stuff for the last character. In the future we might change this to
	// look at specific versions of ConEmu if they fix the bug.
	// We can also try disabling auto margin mode.
	tryVt := true
	wt.truecolor = true
	if os.Getenv("ConEmuPID") != "" {
		wt.truecolor = false
		tryVt = false
	}

	switch tcmode {
	case TruecolorMode_disable:
		wt.truecolor = false
	case TruecolorMode_enable:
		wt.truecolor = true
		tryVt = true
	}

	switch vtmode {
	case VtMode_disable:
		tryVt = false
	case VtMode_enable:
		tryVt = true
	}

	if tryVt {
		wt.setOutMode(wt.outmode | modeVtOutput)
		var om uint32
		wt.getOutMode(&om)
		if om&modeVtOutput == modeVtOutput {
			wt.vten = true
		} else {
			wt.truecolor = false
			// s.setOutMode(0)
		}
	} else {
		// s.setOutMode(0)
	}

	cf, _, e := procCreateEvent.Call(
		uintptr(0),
		uintptr(1),
		uintptr(0),
		uintptr(0))
	if cf == uintptr(0) {
		return nil, e
	}
	wt.cancelflag = syscall.Handle(cf)

	wt.termMode = TerminalModeCooked
	wt.inmode = modeCooked | modeEnableLineInput | modeEnableEchoInput |
		modeResizeEn | modeMouseEn | modeEnableInsertMode | modeEnableQuickEdit | modeExtendFlg
	// modeVtInput modeEnableAutoPosition modeEnableQuickEdit
	wt.setInMode(wt.inmode)

	wt.outmode = modeCookedOut | modeWrapEOL | modeVtOutput | modeUnderline
	//modeNoAutoNL
	wt.setOutMode(wt.outmode)

	wt.eventChan = make(chan Event)
	wt.quit = make(chan struct{})
	// s.scandone = make(chan struct{})
	// s.stopQ = make(chan struct{})
	go wt.runEventLoop()

	return &wt, nil
}

func (wt *windowsTerminal) GetModes() TerminalMode {
	return wt.termMode
}

func (wt *windowsTerminal) SetModes(mode TerminalMode) {
	wt.termMode = mode

	if mode&TerminalModeWrapAtEOL != 0 {
		// set
		wt.outmode |= modeWrapEOL
		wt.outmode &= ^modeNoAutoNL
	} else {
		// clear
		wt.outmode |= modeNoAutoNL
		wt.outmode &= ^modeWrapEOL
	}

	if mode&TerminalModeLineInput != 0 {
		wt.inmode |= modeEnableLineInput
	} else {
		wt.inmode &= ^modeEnableLineInput
	}

	if mode&TerminalModeEchoInput != 0 {
		wt.inmode |= modeEnableEchoInput
	} else {
		wt.inmode &= ^modeEnableEchoInput
	}

	if mode&TerminalModeprocessControlCharacters != 0 {
		wt.inmode |= modeCooked
	} else {
		wt.inmode &= ^modeCooked
	}

	wt.setInMode(wt.inmode)
	wt.setOutMode(wt.outmode)
}

func (wt *windowsTerminal) runEventLoop() {
	for {
		select {
		case <-wt.quit:
			return
		default:
		}
		ev, count, err := wt.getConsoleInput()
		if err != nil {
			return
		}
		for i := 0; i < count; i++ {
			wt.eventChan <- ev
			select {
			case wt.eventChan <- ev:
				//
			case <-wt.quit:
				return
			}
		}
	}
}

func (wt *windowsTerminal) EventChan() <-chan Event {
	return wt.eventChan
}

func (wt *windowsTerminal) getConsoleInput() (Event, int, error) {
	// cancelFlag comes first as WaitForMultipleObjects returns the lowest index
	// in the event that both events are signalled.
	waitObjects := []syscall.Handle{wt.cancelflag, wt.in}
	// As arrays are contiguous in memory, a pointer to the first object is the
	// same as a pointer to the array itself.
	pWaitObjects := unsafe.Pointer(&waitObjects[0])

	rv, _, er := procWaitForMultipleObjects.Call(
		uintptr(len(waitObjects)),
		uintptr(pWaitObjects),
		uintptr(0),
		w32Infinite)
	if er != nil {
		errno, ok := er.(syscall.Errno)
		if !ok {
			return nil, 0, er
		}
		if errno != 0 {
			return nil, 0, er
		}

		// log.Printf("procWaitForMultipleObjects gave err %#v\n", er)
		// return nil, 0, er
	}
	// WaitForMultipleObjects returns WAIT_OBJECT_0 + the index.
	switch rv {
	case w32WaitObject0: // s.cancelFlag
		return nil, 0, errors.New("cancelled")
	case w32WaitObject0 + 1: // s.in
		rec := &inputRecord{}
		var nrec int32
		rv, _, er := procReadConsoleInput.Call(
			uintptr(wt.in),
			uintptr(unsafe.Pointer(rec)),
			uintptr(1),
			uintptr(unsafe.Pointer(&nrec)))
		if rv == 0 {
			return nil, 0, er
		}
		if nrec != 1 {
			return nil, 0, nil
		}
		switch rec.typ {
		case keyEvent:
			isdown := geti32(rec.data[0:])
			repeat := getu16(rec.data[4:])
			kcode := getu16(rec.data[6:])
			// scode := getu16(rec.data[8:])
			ch := getu16(rec.data[10:])
			mod := getu32(rec.data[12:])

			if isdown == 0 || repeat < 1 {
				// it's a key release event, ignore it
				return nil, 0, nil
			}
			if ch != 0 {
				// convert shift+tab to backtab
				if mod2mask(mod) == ModShift && ch == vkTab {
					ev := CreateEventKey(KeyBacktab, 0, ModNone)
					return ev, int(repeat), nil
				}

				ev := CreateEventKey(KeyRune, rune(ch), mod2mask(mod))
				return ev, int(repeat), nil
			}
			key := KeyNUL // impossible on Windows
			ok := false
			if key, ok = vkKeys[kcode]; !ok {
				return nil, 0, nil
			}
			ev := CreateEventKey(key, rune(ch), mod2mask(mod))
			return ev, int(repeat), nil

		case mouseEvent:
			x := geti16(rec.data[0:])
			y := geti16(rec.data[2:])
			btns := getu32(rec.data[4:])
			mod := getu32(rec.data[8:])
			flags := getu32(rec.data[12:])
			btns2 := mrec2btns(btns, flags)
			// we ignore double click, events are delivered normally
			ev := CreateEventMouse(int(x), int(y), btns2, mod2mask(mod))
			return ev, 1, nil

		case resizeEvent:
			x := geti16(rec.data[0:])
			y := geti16(rec.data[2:])
			ev := CreateEventResize(int(x), int(y))
			return ev, 1, nil

		case focusEvent:
			focused := geti32(rec.data[0:])
			ev := CreateEventFocus(focused != 0)
			return ev, 1, nil
		}
	}

	return nil, 0, nil
}

func (wt *windowsTerminal) Write(p []byte) (n int, err error) {
	s := string(p)
	return wt.WriteRunes([]rune(s))
}

func (wt *windowsTerminal) WriteRunes(runes []rune) (int, error) {
	utf16arr := utf16.Encode(runes)
	n, err := wt.WriteUtf16(utf16arr)
	if err != nil {
		return 0, err
	}
	if n == len(utf16arr) {
		// all runes have been written, so we can just return the lenght of the input array
		return len(runes), nil
	}

	// no error has happened, but not the entire rune array has been printed
	// so now we have to calculate how many runes actually have been written
	writtenArr := utf16.Decode(utf16arr[:n])
	return len(writtenArr), nil
}

func (wt *windowsTerminal) WriteUtf16(utf16arr []uint16) (n int, err error) {
	written := uint32(0) // the number of 16 bit elements being written
	err = syscall.WriteConsole(wt.out, &utf16arr[0], uint32(len(utf16arr)), &written, nil)
	if err != nil {
		return 0, err
	}

	return int(written), nil
}

func (wt *windowsTerminal) WriteString(vs string) error {
	esc := utf16.Encode([]rune(vs))
	err := syscall.WriteConsole(wt.out, &esc[0], uint32(len(esc)), nil, nil)
	return err
}

func (wt *windowsTerminal) Read(p []byte) (int, error) {
	// if len(p) == 0 {
	// 	return 0, nil
	// }

	// // we assume that unicode (utf16) support is enabled, so the size of the buffer
	// // is the number of utf16 elements that fit into it.
	// const TCHARSIZE = 2
	// charsread := 0
	// buffersize := len(p) / TCHARSIZE
	// _, _, err := procReadConsole.Call(uintptr(winterm.in),
	// 	uintptr(unsafe.Pointer(&p[0])),
	// 	uintptr(buffersize),
	// 	uintptr(unsafe.Pointer(&charsread)))
	// if err != nil {
	// 	errno, ok := err.(syscall.Errno)
	// 	if ok && errno == 0 {
	// 		err = nil
	// 	}
	// }

	return syscall.Read(wt.in, p)

	// return charsread * TCHARSIZE, err
}

func (wt *windowsTerminal) getConsoleInfo(info *consoleInfo) {
	_, _, _ = procGetConsoleScreenBufferInfo.Call(
		uintptr(wt.out),
		uintptr(unsafe.Pointer(info)))
}

func (wt *windowsTerminal) getCursorInfo(info *cursorInfo) {
	_, _, _ = procGetConsoleCursorInfo.Call(
		uintptr(wt.out),
		uintptr(unsafe.Pointer(info)))
}

func (wt *windowsTerminal) setInMode(mode uint32) {
	_, _, _ = procSetConsoleMode.Call(
		uintptr(wt.in),
		uintptr(mode))
}

func (wt *windowsTerminal) setOutMode(mode uint32) {
	_, _, _ = procSetConsoleMode.Call(
		uintptr(wt.out),
		uintptr(mode))
}

func (wt *windowsTerminal) getInMode(v *uint32) {
	_, _, _ = procGetConsoleMode.Call(
		uintptr(wt.in),
		uintptr(unsafe.Pointer(v)))
}

func (wt *windowsTerminal) getOutMode(v *uint32) {
	_, _, _ = procGetConsoleMode.Call(
		uintptr(wt.out),
		uintptr(unsafe.Pointer(v)))
}

func (wt *windowsTerminal) setCursorInfo(info *cursorInfo) {
	_, _, _ = procSetConsoleCursorInfo.Call(
		uintptr(wt.out),
		uintptr(unsafe.Pointer(info)))
}

func (wt *windowsTerminal) EnableAltMode() {
	if wt.vten {
		wt.setOutMode(modeVtOutput | modeNoAutoNL | modeCookedOut | modeUnderline)
		wt.WriteString(vtEnterCA)
		wt.WriteString(vtDisableAm)
	} else {
		// s.setOutMode(0)
	}
}

func (wt *windowsTerminal) DisableAltMode() {
	if wt.vten {
		wt.WriteString(vtExitCA)
		wt.WriteString(vtEnableAm)
	}
}

func (wt *windowsTerminal) SetStyle(s Style) {
	if wt.vten {
		wt.sendVtStyle(s)
	} else {
		_, _, _ = procSetConsoleTextAttribute.Call(
			uintptr(wt.out),
			uintptr(mapStyle(wt.oscreen.attrs, s)))
	}
}

func (wt *windowsTerminal) sendVtStyle(style Style) {
	esc := &strings.Builder{}

	fg, bg, attrs := style.fg, style.bg, style.attrs
	us, uc := style.ulStyle, style.ulColor

	esc.WriteString(vtSgr0)
	if attrs&(AttrBold|AttrDim) == AttrBold {
		esc.WriteString(vtBold)
	}
	if attrs&AttrBlink != 0 {
		esc.WriteString(vtBlink)
	}
	if us != UnderlineStyleNone {
		if uc == ColorReset {
			esc.WriteString(vtUnderColorReset)
		} else if uc.IsRGB() {
			r, g, b := uc.RGB()
			_, _ = fmt.Fprintf(esc, vtUnderColorRGB, int(r), int(g), int(b))
		} else if uc.Valid() {
			_, _ = fmt.Fprintf(esc, vtUnderColor, uc&0xff)
		}

		esc.WriteString(vtUnderline)
		// legacy ConHost does not understand these but Terminal does
		switch us {
		case UnderlineStyleSolid:
		case UnderlineStyleDouble:
			esc.WriteString(vtDoubleUnderline)
		case UnderlineStyleCurly:
			esc.WriteString(vtCurlyUnderline)
		case UnderlineStyleDotted:
			esc.WriteString(vtDottedUnderline)
		case UnderlineStyleDashed:
			esc.WriteString(vtDashedUnderline)
		}
	}

	if attrs&AttrReverse != 0 {
		esc.WriteString(vtReverse)
	}
	if fg.IsRGB() {
		r, g, b := fg.RGB()
		_, _ = fmt.Fprintf(esc, vtSetFgRGB, r, g, b)
	} else if fg.Valid() {
		_, _ = fmt.Fprintf(esc, vtSetFg, fg&0xff)
	}
	if bg.IsRGB() {
		r, g, b := bg.RGB()
		_, _ = fmt.Fprintf(esc, vtSetBgRGB, r, g, b)
	} else if bg.Valid() {
		_, _ = fmt.Fprintf(esc, vtSetBg, bg&0xff)
	}
	// URL string can be long, so don't send it unless we really need to
	if style.url != "" {
		_, _ = fmt.Fprintf(esc, vtEnterUrl, style.urlId, style.url)
	} else {
		esc.WriteString(vtExitUrl)
	}

	wt.WriteString(esc.String())
}

func (wt *windowsTerminal) SetTitle(title string) {
	if wt.vten {
		wt.WriteString(fmt.Sprintf(vtSetTitle, title))
	}
}

func (wt *windowsTerminal) ResetTitle() {
	if wt.vten {
		wt.WriteString(fmt.Sprintf(vtSetTitle, ""))
	}
}

func (wt *windowsTerminal) ClearScreen(clearScrollbackbuffer bool) {
	if wt.vten {
		if clearScrollbackbuffer {
			wt.WriteString(vtClearScrollbackbuffer)
		} else {
			wt.WriteString(vtClearScreen)
		}
		// s.R
	} else {
		pos := coord{0, 0}
		attr := mapStyle(wt.oscreen.attrs, StyleDefault)
		x, y := wt.Size()
		scratch := uint32(0)
		count := uint32(x * y)

		_, _, _ = procFillConsoleOutputAttribute.Call(
			uintptr(wt.out),
			uintptr(attr),
			uintptr(count),
			pos.uintptr(),
			uintptr(unsafe.Pointer(&scratch)))
		_, _, _ = procFillConsoleOutputCharacter.Call(
			uintptr(wt.out),
			uintptr(' '),
			uintptr(count),
			pos.uintptr(),
			uintptr(unsafe.Pointer(&scratch)))
	}
}

func (wt *windowsTerminal) ShowCursor() {
	if wt.vten {
		wt.WriteString(vtShowCursor)
		// s.WriteString(vtCursorStyles[s.cursorStyle])
		// if s.cursorColor == ColorReset {
		// 	s.WriteString(vtCursorColorReset)
		// } else if s.cursorColor.Valid() {
		// 	r, g, b := s.cursorColor.RGB()
		// 	s.WriteString(fmt.Sprintf(vtCursorColorRGB, r, g, b))
		// }
	} else {
		wt.setCursorInfo(&cursorInfo{size: 100, visible: 1})
	}
}

func (wt *windowsTerminal) HideCursor() {
	if wt.vten {
		wt.WriteString(vtHideCursor)
	} else {
		wt.setCursorInfo(&cursorInfo{size: 1, visible: 0})
	}
}

func (wt *windowsTerminal) CursorPos() (int, int) {
	info := consoleInfo{}
	wt.getConsoleInfo(&info)
	return int(info.pos.x), int(info.pos.y)
}

func (wt *windowsTerminal) SetCursorPos(x, y int) {
	if wt.vten {
		// Note that the string is Y first.  Origin is 1,1.
		wt.WriteString(fmt.Sprintf(vtCursorPos, y+1, x+1))
	} else {
		_, _, _ = procSetConsoleCursorPosition.Call(
			uintptr(wt.out),
			coord{int16(x), int16(y)}.uintptr())
	}
}

func (wt *windowsTerminal) MoveCursor(x, y int) {
	if x > 0 {
		wt.WriteString(fmt.Sprintf(vtMoveCursorRight, x))
	}
	if x < 0 {
		wt.WriteString(fmt.Sprintf(vtMoveCursorLeft, -x))
	}
	if y > 0 {
		wt.WriteString(fmt.Sprintf(vtMoveCursorDown, y))
	}
	if y < 0 {
		wt.WriteString(fmt.Sprintf(vtMoveCursorUp, -y))
	}
}

func (wt *windowsTerminal) ResetCursorPos() {
	if wt.vten {
		wt.WriteString(vtResetCursorpos)
	} else {
		wt.SetCursorPos(0, 0)
	}
}

func (wt *windowsTerminal) SetCursorStyle(cs CursorStyle, cc Color) {
	if wt.vten {
		// s.WriteString(vtShowCursor)
		wt.WriteString(vtCursorStyles[cs])
		if cc == ColorReset {
			wt.WriteString(vtCursorColorReset)
		} else if cc.Valid() {
			r, g, b := cc.RGB()
			wt.WriteString(fmt.Sprintf(vtCursorColorRGB, r, g, b))
		}
	}
}

func (wt *windowsTerminal) retrieveSize() {
	info := consoleInfo{}
	wt.getConsoleInfo(&info)

	w := int((info.win.right - info.win.left) + 1)
	h := int((info.win.bottom - info.win.top) + 1)

	r := rect{0, 0, int16(w - 1), int16(h - 1)}
	_, _, _ = procSetConsoleWindowInfo.Call(
		uintptr(wt.out),
		uintptr(1),
		uintptr(unsafe.Pointer(&r)))

	wt.width, wt.height = w, h
}

func (wt *windowsTerminal) Size() (int, int) {
	return wt.width, wt.height
}

func (wt *windowsTerminal) SetSize(w, h int) {
	oldWidth, oldHeight := wt.width, wt.height

	xy, _, _ := procGetLargestConsoleWindowSize.Call(uintptr(wt.out))

	// xy is little endian packed
	y := int(xy >> 16)
	x := int(xy & 0xffff)

	if x == 0 || y == 0 {
		return
	}

	// This is a hacky workaround for Windows Terminal.
	// Essentially Windows Terminal (Windows 11) does not support application
	// initiated resizing.  To detect this, we look for an extremely large size
	// for the maximum width.  If it is > 500, then this is almost certainly
	// Windows Terminal, and won't support this.  (Note that the legacy console
	// does support application resizing.)
	if x >= 500 {
		return
	}

	wt.setBufferSize(x, y)
	r := rect{0, 0, int16(w - 1), int16(h - 1)}
	_, _, _ = procSetConsoleWindowInfo.Call(
		uintptr(wt.out),
		uintptr(1),
		uintptr(unsafe.Pointer(&r)))

	wt.retrieveSize()

	if wt.width != oldWidth || wt.height != oldHeight {
		// size changed...
	}
}

func (wt *windowsTerminal) setBufferSize(x, y int) {
	_, _, _ = procSetConsoleScreenBufferSize.Call(
		uintptr(wt.out),
		coord{int16(x), int16(y)}.uintptr())
}

func (wt *windowsTerminal) HasMouse() bool {
	return true
}

func (wt *windowsTerminal) EnableMouse() {
	wt.inmode |= modeMouseEn
	wt.inmode &= ^modeEnableQuickEdit
	// s.setInMode(modeResizeEn | modeMouseEn | modeExtendFlg)
	wt.setInMode(wt.inmode)
}

func (wt *windowsTerminal) DisableMouse() {
	wt.inmode &= ^modeMouseEn
	wt.inmode |= modeEnableQuickEdit
	// s.setInMode(modeResizeEn | modeExtendFlg)
	wt.setInMode(wt.inmode)
}

func (wt *windowsTerminal) HasKey(k Key) bool {
	// Microsoft has codes for some keys, but they are unusual,
	// so we don't include them.  We include all the typical
	// 101, 105 key layout keys.
	valid := map[Key]struct{}{
		KeyBackspace: {},
		KeyTab:       {},
		KeyEscape:    {},
		KeyPause:     {},
		KeyPrint:     {},
		KeyPgUp:      {},
		KeyPgDn:      {},
		KeyEnter:     {},
		KeyEnd:       {},
		KeyHome:      {},
		KeyLeft:      {},
		KeyUp:        {},
		KeyRight:     {},
		KeyDown:      {},
		KeyInsert:    {},
		KeyDelete:    {},
		KeyF1:        {},
		KeyF2:        {},
		KeyF3:        {},
		KeyF4:        {},
		KeyF5:        {},
		KeyF6:        {},
		KeyF7:        {},
		KeyF8:        {},
		KeyF9:        {},
		KeyF10:       {},
		KeyF11:       {},
		KeyF12:       {},
		KeyRune:      {},
	}

	_, ok := valid[k]
	if !ok {
		return false
	}

	return true
}

func (wt *windowsTerminal) Beep() error {
	// A simple beep. If the sound card is not available, the sound is generated
	// using the speaker.
	//
	// Reference:
	// https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-messagebeep
	const simpleBeep = 0xffffffff
	if rv, _, err := procMessageBeep.Call(simpleBeep); rv == 0 {
		return err
	}
	return nil
}

func (wt *windowsTerminal) SetClipboard(_ []byte) {
}

func (wt *windowsTerminal) GetClipboard() {
}

func (wt *windowsTerminal) Colors() int {
	if wt.vten {
		return 1 << 24
	}
	// Windows console can display 8 colors, in either low or high intensity
	return 16
}

func (wt *windowsTerminal) CharacterSet() string {
	// We are always UTF-16LE on Windows
	return "UTF-16LE"
}

func (wt *windowsTerminal) CanDisplay(_ rune) bool {
	// We presume we can display anything -- we're Unicode.
	// (Sadly this not precisely true.  Combining characters are especially
	// poorly supported under Windows.)
	return true
}

// Windows lacks bracketed paste (for now)
func (wt *windowsTerminal) EnablePaste() {}

func (wt *windowsTerminal) DisablePaste() {}

func (wt *windowsTerminal) Close() error {
	// TODO: check if terminal is already closed

	wt.ClearScreen(false)
	wt.ResetCursorPos()

	if wt.vten {
		wt.WriteString(vtCursorStyles[CursorStyleDefault])
		wt.WriteString(vtCursorColorReset)
	}
	wt.DisableAltMode()
	wt.ResetTitle()

	// s.setCursorInfo(&s.ocursor)
	// s.setBufferSize(int(s.oscreen.size.x), int(s.oscreen.size.y))
	wt.setInMode(wt.origInmode)
	wt.setOutMode(wt.origOutmode)

	_, _, _ = procSetEvent.Call(uintptr(wt.cancelflag))
	_, _, _ = procSetConsoleTextAttribute.Call(
		uintptr(wt.out),
		uintptr(mapStyle(wt.oscreen.attrs, StyleDefault)))

	errIn := syscall.Close(wt.in)
	wt.in = 0

	errOut := syscall.Close(wt.out)
	wt.out = 0

	if errIn != nil {
		return errIn
	}

	if errOut != nil {
		return errOut
	}

	return nil
}

// Map a tcell style to Windows attributes
func mapStyle(oldAttrs uint16, style Style) uint16 {
	// f, b, a := style.fg, style.bg, style.attrs
	fa := oldAttrs & 0xf
	ba := (oldAttrs) >> 4 & 0xf
	if style.fg != ColorDefault && style.fg != ColorReset {
		fa = mapColor2RGB(style.fg)
	}
	if style.bg != ColorDefault && style.bg != ColorReset {
		ba = mapColor2RGB(style.bg)
	}
	var attr uint16
	// We simulate reverse by doing the color swap ourselves.
	// Apparently windows cannot really do this except in DBCS
	// views.
	if style.attrs&AttrReverse != 0 {
		attr = ba
		attr |= fa << 4
	} else {
		attr = fa
		attr |= ba << 4
	}
	if style.attrs&AttrBold != 0 {
		attr |= 0x8
	}
	if style.attrs&AttrDim != 0 {
		attr &^= 0x8
	}
	if style.attrs&AttrUnderline != 0 {
		// Best effort -- doesn't seem to work though.
		attr |= 0x8000
	}
	// Blink is unsupported
	return attr
}

// Windows uses RGB signals
func mapColor2RGB(c Color) uint16 {
	winLock.Lock()
	if v, ok := winColors[c]; ok {
		c = v
	} else {
		v = FindColor(c, winPalette)
		winColors[c] = v
		c = v
	}
	winLock.Unlock()

	if vc, ok := vgaColors[c]; ok {
		return vc
	}
	return 0
}
