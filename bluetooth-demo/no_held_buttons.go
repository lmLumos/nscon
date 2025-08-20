// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"bufio"
	"fmt"
	"github.com/mzyy94/nscon"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Controller state to track button presses and releases
type ControllerState struct {
	buttons map[string]bool
	axes    map[string]float64
}

func NewControllerState() *ControllerState {
	return &ControllerState{
		buttons: make(map[string]bool),
		axes:    make(map[string]float64),
	}
}

func setInput(input *uint8) {
	*input = 1
	time.AfterFunc(100*time.Millisecond, func() {
		*input = 0
	})
}

func setAnalogStick(x, y *float64, axisX, axisY float64) {
	// Convert from typical gamepad range (-1.0 to 1.0) to Nintendo Switch range (-1.0 to 1.0)
	*x = axisX
	*y = -axisY // Invert Y axis as Nintendo Switch has inverted Y
}

// Read events from the wireless controller device
func readControllerEvents(devicePath string, con *nscon.Controller, state *ControllerState) {
	file, err := os.Open(devicePath)
	if err != nil {
		log.Fatalf("Failed to open controller device %s: %v", devicePath, err)
	}
	defer file.Close()

	log.Printf("Reading controller events from %s", devicePath)

	// This is a simplified example - in reality you'd need to parse the actual
	// input event structure. For demonstration, we'll read line-based input
	scanner := bufio.NewScanner(file)
	
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		
		// Parse controller input (format: "type:code:value")
		parts := strings.Split(line, ":")
		if len(parts) != 3 {
			continue
		}
		
		eventType := parts[0]
		code := parts[1]
		valueStr := parts[2]
		
		value, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			continue
		}
		
		processControllerInput(eventType, code, value, con, state)
	}
}

func processControllerInput(eventType, code string, value float64, con *nscon.Controller, state *ControllerState) {
	switch eventType {
	case "BTN": // Button events
		pressed := value > 0
		state.buttons[code] = pressed
		
		switch code {
		case "BTN_SOUTH": // A button (Xbox: A, PS: X)
			if pressed {
				setInput(&con.Input.Button.A)
			}
		case "BTN_EAST": // B button (Xbox: B, PS: Circle)
			if pressed {
				setInput(&con.Input.Button.B)
			}
		case "BTN_WEST": // X button (Xbox: X, PS: Square)
			if pressed {
				setInput(&con.Input.Button.X)
			}
		case "BTN_NORTH": // Y button (Xbox: Y, PS: Triangle)
			if pressed {
				setInput(&con.Input.Button.Y)
			}
		case "BTN_TL": // L shoulder button
			if pressed {
				setInput(&con.Input.Button.L)
			}
		case "BTN_TR": // R shoulder button
			if pressed {
				setInput(&con.Input.Button.R)
			}
		case "BTN_TL2": // ZL trigger
			if pressed {
				setInput(&con.Input.Button.ZL)
			}
		case "BTN_TR2": // ZR trigger
			if pressed {
				setInput(&con.Input.Button.ZR)
			}
		case "BTN_SELECT": // Minus/Select button
			if pressed {
				setInput(&con.Input.Button.Minus)
			}
		case "BTN_START": // Plus/Start button
			if pressed {
				setInput(&con.Input.Button.Plus)
			}
		case "BTN_MODE": // Home button
			if pressed {
				setInput(&con.Input.Button.Home)
			}
		case "BTN_THUMBL": // Left stick press
			con.Input.Stick.Left.Press = uint8(value)
		case "BTN_THUMBR": // Right stick press
			con.Input.Stick.Right.Press = uint8(value)
		}
		
	case "ABS": // Absolute axis events (analog sticks, triggers, d-pad)
		state.axes[code] = value
		
		switch code {
		case "ABS_X": // Left stick X
			setAnalogStick(&con.Input.Stick.Left.X, &con.Input.Stick.Left.Y, 
				value, state.axes["ABS_Y"])
		case "ABS_Y": // Left stick Y
			setAnalogStick(&con.Input.Stick.Left.X, &con.Input.Stick.Left.Y, 
				state.axes["ABS_X"], value)
		case "ABS_RX": // Right stick X
			setAnalogStick(&con.Input.Stick.Right.X, &con.Input.Stick.Right.Y, 
				value, state.axes["ABS_RY"])
		case "ABS_RY": // Right stick Y
			setAnalogStick(&con.Input.Stick.Right.X, &con.Input.Stick.Right.Y, 
				state.axes["ABS_RX"], value)
		case "ABS_HAT0X": // D-pad horizontal
			if value < 0 {
				setInput(&con.Input.Dpad.Left)
			} else if value > 0 {
				setInput(&con.Input.Dpad.Right)
			}
		case "ABS_HAT0Y": // D-pad vertical
			if value < 0 {
				setInput(&con.Input.Dpad.Up)
			} else if value > 0 {
				setInput(&con.Input.Dpad.Down)
			}
		}
	}
}

// Alternative implementation using /dev/input/eventX directly
func readInputEvents(devicePath string, con *nscon.Controller) {
	file, err := os.Open(devicePath)
	if err != nil {
		log.Fatalf("Failed to open input device %s: %v", devicePath, err)
	}
	defer file.Close()

	log.Printf("Reading input events from %s", devicePath)

	// Buffer for input_event struct (typically 24 bytes on 64-bit systems)
	// struct input_event {
	//     struct timeval time; (16 bytes)
	//     __u16 type;         (2 bytes)
	//     __u16 code;         (2 bytes)
	//     __s32 value;        (4 bytes)
	// }
	eventSize := 24
	buffer := make([]byte, eventSize)
	
	state := NewControllerState()

	for {
		n, err := file.Read(buffer)
		if err != nil {
			log.Printf("Error reading from device: %v", err)
			continue
		}
		
		if n != eventSize {
			continue
		}

		// Parse the input_event structure
		eventType := uint16(buffer[16]) | uint16(buffer[17])<<8
		code := uint16(buffer[18]) | uint16(buffer[19])<<8
		value := int32(buffer[20]) | int32(buffer[21])<<8 | int32(buffer[22])<<16 | int32(buffer[23])<<24

		// Map Linux input codes to our controller
		handleInputEvent(eventType, code, value, con, state)
	}
}

func handleInputEvent(eventType uint16, code uint16, value int32, con *nscon.Controller, state *ControllerState) {
	const (
		EV_KEY = 1 // Button events
		EV_ABS = 3 // Absolute axis events
		EV_SYN = 0 // Sync events
	)

	switch eventType {
	case EV_KEY:
		pressed := value > 0
		
		switch code {
		case 304: // BTN_SOUTH (A)
			if pressed {
				setInput(&con.Input.Button.A)
			}
		case 305: // BTN_EAST (B)
			if pressed {
				setInput(&con.Input.Button.B)
			}
		case 307: // BTN_NORTH (Y)
			if pressed {
				setInput(&con.Input.Button.Y)
			}
		case 308: // BTN_WEST (X)
			if pressed {
				setInput(&con.Input.Button.X)
			}
		case 310: // BTN_TL (L)
			if pressed {
				setInput(&con.Input.Button.L)
			}
		case 311: // BTN_TR (R)
			if pressed {
				setInput(&con.Input.Button.R)
			}
		case 312: // BTN_TL2 (ZL)
			if pressed {
				setInput(&con.Input.Button.ZL)
			}
		case 313: // BTN_TR2 (ZR)
			if pressed {
				setInput(&con.Input.Button.ZR)
			}
		case 314: // BTN_SELECT (Minus)
			if pressed {
				setInput(&con.Input.Button.Minus)
			}
		case 315: // BTN_START (Plus)
			if pressed {
				setInput(&con.Input.Button.Plus)
			}
		case 316: // BTN_MODE (Home)
			if pressed {
				setInput(&con.Input.Button.Home)
			}
		case 317: // BTN_THUMBL (Left stick press)
			con.Input.Stick.Left.Press = uint8(value)
		case 318: // BTN_THUMBR (Right stick press)
			con.Input.Stick.Right.Press = uint8(value)
		}

	case EV_ABS:
		// Debug output to see raw values
		if con.LogLevel > 1 {
			log.Printf("Axis event - Code: %d, Raw Value: %d", code, value)
		}
		
		// Your controller uses 8-bit range (0-255) with ~127-128 as center
		var normalizedValue float64
		
		// Based on your debug output, values are around 126-127 for center
		// This indicates 8-bit unsigned range (0-255) with 127.5 as center
		if value >= 0 && value <= 255 {
			// 8-bit unsigned range (0 to 255), convert to -1.0 to 1.0
			// Center should be around 127.5, so we use 127.5 as neutral
			normalizedValue = (float64(value) - 127.5) / 127.5
		} else if value >= -32768 && value <= 32767 {
			// Standard signed 16-bit range (-32768 to 32767)
			normalizedValue = float64(value) / 32767.0
		} else if value >= 0 && value <= 4095 {
			// 12-bit unsigned range, convert to -1.0 to 1.0
			normalizedValue = (float64(value) - 2048.0) / 2048.0
		} else if value >= 0 && value <= 1023 {
			// 10-bit unsigned range, convert to -1.0 to 1.0
			normalizedValue = (float64(value) - 512.0) / 512.0
		} else {
			// Fallback: assume 8-bit unsigned since that's what we're seeing
			normalizedValue = (float64(value) - 127.5) / 127.5
		}

		// Clamp to valid range
		if normalizedValue > 1.0 {
			normalizedValue = 1.0
		} else if normalizedValue < -1.0 {
			normalizedValue = -1.0
		}
		
		// Apply deadzone (ignore very small movements near center)
		// For 8-bit controllers, deadzone should be smaller since resolution is lower
		if normalizedValue > -0.05 && normalizedValue < 0.05 {
			normalizedValue = 0.0
		}

		switch code {
		case 0: // ABS_X (Left stick X)
			con.Input.Stick.Left.X = normalizedValue
			if con.LogLevel > 1 {
				log.Printf("Left Stick X: raw=%d, normalized=%.3f", value, normalizedValue)
			}
		case 1: // ABS_Y (Left stick Y)  
			con.Input.Stick.Left.Y = -normalizedValue // Invert Y
			if con.LogLevel > 1 {
				log.Printf("Left Stick Y: raw=%d, normalized=%.3f (inverted)", value, -normalizedValue)
			}
		case 3: // ABS_RX (Right stick X)
			con.Input.Stick.Right.X = normalizedValue
			if con.LogLevel > 1 {
				log.Printf("Right Stick X: raw=%d, normalized=%.3f", value, normalizedValue)
			}
		case 4: // ABS_RY (Right stick Y)
			con.Input.Stick.Right.Y = -normalizedValue // Invert Y  
			if con.LogLevel > 1 {
				log.Printf("Right Stick Y: raw=%d, normalized=%.3f (inverted)", value, -normalizedValue)
			}
		case 2: // ABS_Z (Left trigger on some controllers)
			// Some controllers map triggers to Z/RZ
			log.Printf("Left trigger (ABS_Z): %d", value)
		case 5: // ABS_RZ (Right trigger on some controllers)
			log.Printf("Right trigger (ABS_RZ): %d", value)
		case 16: // ABS_HAT0X (D-pad horizontal)
			if value < 0 {
				setInput(&con.Input.Dpad.Left)
			} else if value > 0 {
				setInput(&con.Input.Dpad.Right)
			}
		case 17: // ABS_HAT0Y (D-pad vertical)
			if value < 0 {
				setInput(&con.Input.Dpad.Up)
			} else if value > 0 {
				setInput(&con.Input.Dpad.Down)
			}
		default:
			if con.LogLevel > 1 {
				log.Printf("Unknown axis code %d with value %d", code, value)
			}
		}
	case EV_SYN:
		// Sync events - can be ignored but useful for debugging
		if con.LogLevel > 2 {
			log.Printf("Sync event")
		}
	}
}

func findControllerDevice() string {
	// Common paths for Bluetooth controllers
	possiblePaths := []string{
		"/dev/input/event0",
		"/dev/input/event1",
		"/dev/input/event2",
		"/dev/input/event3",
		"/dev/input/event4",
		"/dev/input/event5",
		"/dev/input/js0", // Joystick interface
		"/dev/input/js1",
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			log.Printf("Found potential controller device: %s", path)
			return path
		}
	}

	log.Println("No controller device found, using /dev/input/event0")
	return "/dev/input/event0"
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-h" || os.Args[1] == "--help") {
		fmt.Println("Usage: sudo go run bluetooth_main.go [device_path] [--debug]")
		fmt.Println("Make sure your Bluetooth controller is paired and connected.")
		fmt.Println("Options:")
		fmt.Println("  device_path  Path to input device (e.g. /dev/input/event2)")
		fmt.Println("  --debug      Show detailed axis debugging info")
		fmt.Println("")
		fmt.Println("To find your controller device, run: sudo evtest")
		return
	}

	target := "/dev/hidg0"
	con := nscon.NewController(target)
	
	// Enable debug mode if requested
	debugMode := false
	for _, arg := range os.Args {
		if arg == "--debug" {
			debugMode = true
			con.LogLevel = 3 // Maximum logging
			break
		}
	}
	
	if !debugMode {
		con.LogLevel = 2
	}
	
	defer con.Close()
	
	err := con.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to Nintendo Switch controller: %v", err)
	}

	// Find controller device
	var controllerDevice string
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "--") {
		controllerDevice = os.Args[1]
	} else {
		controllerDevice = findControllerDevice()
	}

	log.Printf("Using controller device: %s", controllerDevice)
	log.Println("Make sure your Bluetooth controller is connected and the device path is correct.")
	
	if debugMode {
		log.Println("DEBUG MODE: Move your analog sticks to see raw values...")
	}
	
	log.Println("Press Ctrl+C to exit.")

	// Start reading controller input in a goroutine
	go readInputEvents(controllerDevice, con)

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c
	log.Println("Shutting down...")
}