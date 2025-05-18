//go:build !windows

package bufferless

func NewWindowsTerminal(tcmode TruecolorMode, vtmode VtMode) (Terminal, error) {
	return nil, ErrNoTerminal
}
