package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gdamore/tcell/v2/bufferless"
)

func main() {
	term, err := bufferless.NewWindowsTerminal(bufferless.TruecolorMode_system, bufferless.VtMode_system)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return
	}
	defer term.Close()

	// term.EnableMouse()
	// mode :=
	// 	bufferless.TerminalModeWrapAtEOL |
	// 		bufferless.TerminalModeLineInput |
	// 		bufferless.TerminalModeEchoInput |
	// 		bufferless.TerminalModeprocessControlCharacters |
	// 		0
	// term.SetModes(mode)

	// go loop(term)

	loop := true
	for loop {

		fmt.Println("choose an option:\r")

		buffer := [16]byte{}
		// n, err := os.Stdin.Read(buffer[:])
		n, err := term.Read(buffer[:])
		if err != nil {
			fmt.Printf("Read gave error ")
			log.Fatal(err)
		}
		fmt.Printf("read %v bytes.\n", n)

		// 	// reader := bufio.NewReader(os.Stdin)
		// 	// line, err := reader.ReadString('\n')
		// 	if err != nil {
		// 		log.Fatal(err)
		// 	}
		line := string(buffer[:n])
		switch line {
		// 	case "1\r\n":
		// 		// fmt.Println("1 was pressed\r")
		// 		term.SetTitle("Title test")

		// 	case "2\r\n":
		// 		term.SetTitle("Title test 2")

		// 	case "3\r\n":
		// 		term.SetTitle("")

		// 	case "4\r\n":
		// 		term.ClearScreen(false)
		// 		term.ResetCursorPos()

		// 	case "5\r\n":
		// 		term.ClearScreen(true)
		// 		term.ResetCursorPos()

		// 	case "6\r\n":
		// 		term.EnableAltMode()

		// 	case "7\r\n":
		// 		term.DisableAltMode()

		case "q\r\n", "quit\r\n", "exit\r\n", "q":
			loop = false

		default:
			fmt.Printf("%#v is not valid input\r\n", line)
		}
	}

	// fmt.Println("quiting\r")

	// unicode4byte := "𒀤"
	// unicode4byte := "𒀀"
	// i, err := term.WriteString(unicode4byte)
	// if err != nil {
	// 	fmt.Fprintf(os.Stderr, "%v\n", err)
	// 	return
	// }
	// fmt.Println(i)
}

func loop(term bufferless.Terminal) {
	for e := range term.EventChan() {
		term.ResetCursorPos()
		log.Printf("recieved event: %#v\n", e)
		switch ev := e.(type) {
		case bufferless.EventKey:
			if ev.Key() == bufferless.KeyESC {
				// return
			}
		}
	}
}
