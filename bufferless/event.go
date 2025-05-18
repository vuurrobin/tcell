package bufferless

// Event is a generic interface used for passing around Events.
// Concrete types follow.
type Event interface {
}

// EventHandler is anything that handles events.  If the handler has
// consumed the event, it should return true.  False otherwise.
type EventHandler interface {
	HandleEvent(Event) bool
}

// EventResize is sent when the window size changes.
type EventResize struct {
	ws WindowSize
}

// CreateEventResize creates an EventResize with the new updated window size,
// which is given in character cells.
func CreateEventResize(width, height int) EventResize {
	ws := WindowSize{
		Width:  width,
		Height: height,
	}
	return EventResize{ws: ws}
}

// Size returns the new window size as width, height in character cells.
func (ev *EventResize) Size() (int, int) {
	return ev.ws.Width, ev.ws.Height
}

// PixelSize returns the new window size as width, height in pixels. The size
// will be 0,0 if the screen doesn't support this feature
func (ev *EventResize) PixelSize() (int, int) {
	return ev.ws.PixelWidth, ev.ws.PixelHeight
}

type WindowSize struct {
	Width       int
	Height      int
	PixelWidth  int
	PixelHeight int
}

// CellDimensions returns the dimensions of a single cell, in pixels
func (ws WindowSize) CellDimensions() (int, int) {
	if ws.PixelWidth == 0 || ws.PixelHeight == 0 {
		return 0, 0
	}
	return (ws.PixelWidth / ws.Width), (ws.PixelHeight / ws.Height)
}

// EventFocus is a focus event. It is sent when the terminal window (or tab)
// gets or loses focus.
type EventFocus struct {
	// True if the window received focus, false if it lost focus
	Focused bool
}

func CreateEventFocus(focused bool) EventFocus {
	return EventFocus{Focused: focused}
}

// ButtonMask is a mask of mouse buttons and wheel events.  Mouse button presses
// are normally delivered as both press and release events.  Mouse wheel events
// are normally just single impulse events.  Windows supports up to eight
// separate buttons plus all four wheel directions, but XTerm can only support
// mouse buttons 1-3 and wheel up/down.  Its not unheard of for terminals
// to support only one or two buttons (think Macs).  Old terminals, and true
// emulations (such as vt100) won't support mice at all, of course.
type ButtonMask int16

// These are the actual button values.  Note that tcell version 1.x reversed buttons
// two and three on *nix based terminals.  We use button 1 as the primary, and
// button 2 as the secondary, and button 3 (which is often missing) as the middle.
const (
	Button1 ButtonMask = 1 << iota // Usually the left (primary) mouse button.
	Button2                        // Usually the right (secondary) mouse button.
	Button3                        // Usually the middle mouse button.
	Button4                        // Often a side button (thumb/next).
	Button5                        // Often a side button (thumb/prev).
	Button6
	Button7
	Button8
	WheelUp                   // Wheel motion up/away from user.
	WheelDown                 // Wheel motion down/towards user.
	WheelLeft                 // Wheel motion to left.
	WheelRight                // Wheel motion to right.
	ButtonNone ButtonMask = 0 // No button or wheel events.

	ButtonPrimary   = Button1
	ButtonSecondary = Button2
	ButtonMiddle    = Button3
)

// EventMouse is a mouse event.  It is sent on either mouse up or mouse down
// events.  It is also sent on mouse motion events - if the terminal supports
// it.  We make every effort to ensure that mouse release events are delivered.
// Hence, click drag can be identified by a motion event with the mouse down,
// without any intervening button release.  On some terminals only the initiating
// press and terminating release event will be delivered.
//
// Mouse wheel events, when reported, may appear on their own as individual
// impulses; that is, there will normally not be a release event delivered
// for mouse wheel movements.
//
// Most terminals cannot report the state of more than one button at a time --
// and some cannot report motion events unless a button is pressed.
//
// Applications can inspect the time between events to resolve double or
// triple clicks.
type EventMouse struct {
	btn ButtonMask
	mod ModMask
	x   int
	y   int
}

// Buttons returns the list of buttons that were pressed or wheel motions.
func (ev *EventMouse) Buttons() ButtonMask {
	return ev.btn
}

// Modifiers returns a list of keyboard modifiers that were pressed
// with the mouse button(s).
func (ev *EventMouse) Modifiers() ModMask {
	return ev.mod
}

// Position returns the mouse position in character cells.  The origin
// 0, 0 is at the upper left corner.
func (ev *EventMouse) Position() (int, int) {
	return ev.x, ev.y
}

// CreateEventMouse is used to create a new mouse event.  Applications
// shouldn't need to use this; its mostly for screen implementors.
func CreateEventMouse(x, y int, btn ButtonMask, mod ModMask) EventMouse {
	return EventMouse{x: x, y: y, btn: btn, mod: mod}
}
