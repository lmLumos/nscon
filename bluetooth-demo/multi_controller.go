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
	"sync"
	"syscall"
	"time"
)

// ControllerManager manages multiple Nintendo Switch controllers
type ControllerManager struct {
	controllers map[int]*nscon.Controller
	devices     map[int]string
	mutex       sync.RWMutex
	logLevel    int
}

// NewControllerManager creates a new multi-controller manager
func NewControllerManager(logLevel int) *ControllerManager {
	return &ControllerManager{
		controllers: make(map[int]*nscon.Controller),
		devices:     make(map[int]string),
		logLevel:    logLevel,
	}
}

// AddController adds a new controller with the given player number (1-8)
func (cm *ControllerManager) AddController(playerNum int, hidgDevice string, inputDevice string) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if playerNum < 1 || playerNum > 8 {
		return fmt.Errorf("player number must be between 1-8, got %d", playerNum)
	}

	if _, exists := cm.controllers[playerNum]; exists {
		return fmt.Errorf("controller %d already exists", playerNum)
	}

	// Create new Nintendo Switch controller
	controller := nscon.NewController(hidgDevice)
	controller.LogLevel = cm.logLevel

	// Connect to the Nintendo Switch
	err := controller.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect controller %d: %v", playerNum, err)
	}

	cm.controllers[playerNum] = controller
	cm.devices[playerNum] = inputDevice

	log.Printf("Controller %d connected: %s -> %s", playerNum, inputDevice, hidgDevice)

	// Start reading input for this controller in a goroutine
	go cm.readControllerInput(playerNum, inputDevice, controller)

	return nil
}

// RemoveController removes a controller
func (cm *ControllerManager) RemoveController(playerNum int) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if controller, exists := cm.controllers[playerNum]; exists {
		controller.Close()
		delete(cm.controllers, playerNum)
		delete(cm.devices, playerNum)
		log.Printf("Controller %d disconnected", playerNum)
	}
}

// Close closes all controllers
func (cm *ControllerManager) Close() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	for playerNum, controller := range cm.controllers {
		controller.Close()
		log.Printf("Controller %d closed", playerNum)
	}
	
	cm.controllers = make(map[int]*nscon.Controller)
	cm.devices = make(map[int]string)
}

// ListControllers returns a list of active controllers
func (cm *ControllerManager) ListControllers() []int {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	var players []int
	for playerNum := range cm.controllers {
		players = append(players, playerNum)
	}
	return players
}

func (cm *ControllerManager) readControllerInput(playerNum int, devicePath string, con *nscon.Controller) {
	file, err := os.Open(devicePath)
	if err != nil {
		log.Printf("Controller %d: Failed to open device %s: %v", playerNum, devicePath, err)
		return
	}
	defer file.Close()

	log.Printf("Controller %d: Reading input events from %s", playerNum, devicePath)

	// Buffer for input_event struct (24 bytes on 64-bit systems)
	eventSize := 24
	buffer := make([]byte, eventSize)

	for {
		// Check if controller still exists
		cm.mutex.RLock()
		_, exists := cm.controllers[playerNum]
		cm.mutex.RUnlock()
		
		if !exists {
			log.Printf("Controller %d: Stopping input reader", playerNum)
			return
		}

		n, err := file.Read(buffer)
		if err != nil {
			log.Printf("Controller %d: Error reading from device: %v", playerNum, err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if n != eventSize {
			continue
		}

		// Parse the input_event structure
		eventType := uint16(buffer[16]) | uint16(buffer[17])<<8
		code := uint16(buffer[18]) | uint16(buffer[19])<<8
		value := int32(buffer[20]) | int32(buffer[21])<<8 | int32(buffer[22])<<16 | int32(buffer[23])<<24

		// Handle input event
		cm.handleInputEvent(playerNum, eventType, code, value, con)
	}
}

func (cm *ControllerManager) handleInputEvent(playerNum int, eventType uint16, code uint16, value int32, con *nscon.Controller) {
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
			setInput(&con.Input.Button.A, pressed)
		case 305: // BTN_EAST (B)
			setInput(&con.Input.Button.B, pressed)
		case 307: // BTN_NORTH (Y)
			setInput(&con.Input.Button.Y, pressed)
		case 308: // BTN_WEST (X)
			setInput(&con.Input.Button.X, pressed)
		case 310: // BTN_TL (L)
			setInput(&con.Input.Button.L, pressed)
		case 311: // BTN_TR (R)
			setInput(&con.Input.Button.R, pressed)
		case 312: // BTN_TL2 (ZL)
			setInput(&con.Input.Button.ZL, pressed)
		case 313: // BTN_TR2 (ZR)
			setInput(&con.Input.Button.ZR, pressed)
		case 314: // BTN_SELECT (Minus)
			setInput(&con.Input.Button.Minus, pressed)
		case 315: // BTN_START (Plus)
			setInput(&con.Input.Button.Plus, pressed)
		case 316: // BTN_MODE (Home)
			setInput(&con.Input.Button.Home, pressed)
		case 317: // BTN_THUMBL (Left stick press)
			con.Input.Stick.Left.Press = uint8(value)
		case 318: // BTN_THUMBR (Right stick press)
			con.Input.Stick.Right.Press = uint8(value)
		}

		if cm.logLevel > 1 {
			log.Printf("Controller %d: Button event - Code: %d, Pressed: %t", playerNum, code, pressed)
		}

	case EV_ABS:
		// Debug output to see raw values
		if cm.logLevel > 2 {
			log.Printf("Controller %d: Axis event - Code: %d, Raw Value: %d", playerNum, code, value)
		}

		// Normalize axis values for 8-bit controllers (0-255 range)
		var normalizedValue float64

		if value >= 0 && value <= 255 {
			normalizedValue = (float64(value) - 127.5) / 127.5
		} else if value >= -32768 && value <= 32767 {
			normalizedValue = float64(value) / 32767.0
		} else {
			normalizedValue = (float64(value) - 127.5) / 127.5
		}

		// Clamp to valid range
		if normalizedValue > 1.0 {
			normalizedValue = 1.0
		} else if normalizedValue < -1.0 {
			normalizedValue = -1.0
		}

		// Apply deadzone
		if normalizedValue > -0.05 && normalizedValue < 0.05 {
			normalizedValue = 0.0
		}

		switch code {
		case 0: // ABS_X (Left stick X)
			con.Input.Stick.Left.X = normalizedValue
		case 1: // ABS_Y (Left stick Y)
			con.Input.Stick.Left.Y = -normalizedValue // Invert Y
		case 3: // ABS_RX (Right stick X)
			con.Input.Stick.Right.X = normalizedValue
		case 4: // ABS_RY (Right stick Y)
			con.Input.Stick.Right.Y = -normalizedValue // Invert Y
		case 16: // ABS_HAT0X (D-pad horizontal)
			if value < 0 {
				con.Input.Dpad.Left = 1
				con.Input.Dpad.Right = 0
			} else if value > 0 {
				con.Input.Dpad.Left = 0
				con.Input.Dpad.Right = 1
			} else {
				con.Input.Dpad.Left = 0
				con.Input.Dpad.Right = 0
			}
		case 17: // ABS_HAT0Y (D-pad vertical)
			if value < 0 {
				con.Input.Dpad.Up = 1
				con.Input.Dpad.Down = 0
			} else if value > 0 {
				con.Input.Dpad.Up = 0
				con.Input.Dpad.Down = 1
			} else {
				con.Input.Dpad.Up = 0
				con.Input.Dpad.Down = 0
			}
		}
	}
}

func setInput(input *uint8, pressed bool) {
	if pressed {
		*input = 1
	} else {
		*input = 0
	}
}

// findInputDevices automatically detects connected controllers
func findInputDevices() map[int]string {
	devices := make(map[int]string)

	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("/dev/input/event%d", i)
		if _, err := os.Stat(path); err == nil {
			// Check the sysfs "name" file for this event device
			namePath := fmt.Sprintf("/sys/class/input/event%d/device/name", i)
			nameBytes, err := os.ReadFile(namePath)
			if err != nil {
				continue
			}

			name := strings.TrimSpace(string(nameBytes))
			if name == "Wireless Controller" {
				devices[len(devices)+1] = path
			}
		}
	}

	return devices
}

// setupUSBGadgets creates the necessary USB gadget devices
func setupUSBGadgets(numControllers int) []string {
	var hidgDevices []string
	
	log.Println("Setting up USB gadgets...")
	log.Printf("You need to create %d USB gadget devices:", numControllers)
	
	for i := 0; i < numControllers; i++ {
		hidgPath := fmt.Sprintf("/dev/hidg%d", i)
		hidgDevices = append(hidgDevices, hidgPath)
		
		log.Printf("  %d. Create %s using the USB gadget script", i+1, hidgPath)
		log.Printf("     Example: sudo ./add_procon_gadget.sh %d", i)
	}
	
	log.Println()
	log.Println("Make sure all USB gadget devices exist before continuing!")
	
	return hidgDevices
}

func printUsage() {
	fmt.Println("Multi-Controller Nintendo Switch Controller Simulator")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("  sudo go run multi_controller.go [options]")
	fmt.Println("")
	fmt.Println("Options:")
	fmt.Println("  --auto          Auto-detect controllers")
	fmt.Println("  --manual        Manual controller setup")
	fmt.Println("  --debug         Enable debug logging")
	fmt.Println("  --help, -h      Show this help")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("  sudo go run multi_controller.go --auto")
	fmt.Println("  sudo go run multi_controller.go --manual")
	fmt.Println("  sudo go run multi_controller.go --debug --auto")
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-h" || os.Args[1] == "--help") {
		printUsage()
		return
	}

	// Parse arguments
	autoMode := false
	manualMode := false
	debugMode := false

	for _, arg := range os.Args[1:] {
		switch arg {
		case "--auto":
			autoMode = true
		case "--manual":
			manualMode = true
		case "--debug":
			debugMode = true
		}
	}

	// Default to auto mode if nothing specified
	if !autoMode && !manualMode {
		autoMode = true
	}

	logLevel := 1
	if debugMode {
		logLevel = 3
	}

	// Create controller manager
	manager := NewControllerManager(logLevel)
	defer manager.Close()

	if autoMode {
		// Auto-detect mode
		log.Println("Auto-detecting controllers...")
		
		inputDevices := findInputDevices()
		if len(inputDevices) == 0 {
			log.Println("No input devices found!")
			log.Println("Make sure your controllers are connected.")
			return
		}

		log.Printf("Found %d potential input device(s)", len(inputDevices))
		
		// Setup USB gadgets
		hidgDevices := setupUSBGadgets(len(inputDevices))
		
		// Wait for user to set up USB gadgets
		fmt.Print("Press Enter when all USB gadgets are created...")
		bufio.NewReader(os.Stdin).ReadBytes('\n')

		// Add controllers
		playerNum := 1
		for _, inputDevice := range inputDevices {
			if playerNum > len(hidgDevices) {
				break
			}
			
			err := manager.AddController(playerNum, hidgDevices[playerNum-1], inputDevice)
			if err != nil {
				log.Printf("Failed to add controller %d: %v", playerNum, err)
			} else {
				playerNum++
			}
		}

	} else {
		// Manual mode
		log.Println("Manual controller setup mode")
		log.Println("Enter controller configurations (player:input_device:hidg_device)")
		log.Println("Example: 1:/dev/input/event2:/dev/hidg0")
		log.Println("Enter 'done' when finished")

		scanner := bufio.NewScanner(os.Stdin)
		for {
			fmt.Print("Controller config> ")
			if !scanner.Scan() {
				break
			}
			
			line := strings.TrimSpace(scanner.Text())
			if line == "done" || line == "" {
				break
			}

			parts := strings.Split(line, ":")
			if len(parts) != 3 {
				log.Println("Invalid format. Use: player:input_device:hidg_device")
				continue
			}

			playerNum, err := strconv.Atoi(parts[0])
			if err != nil {
				log.Printf("Invalid player number: %s", parts[0])
				continue
			}

			inputDevice := parts[1]
			hidgDevice := parts[2]

			err = manager.AddController(playerNum, hidgDevice, inputDevice)
			if err != nil {
				log.Printf("Failed to add controller: %v", err)
			}
		}
	}

	// Show active controllers
	controllers := manager.ListControllers()
	if len(controllers) == 0 {
		log.Println("No controllers active. Exiting...")
		return
	}

	log.Printf("Active controllers: %v", controllers)
	log.Println("All controllers are ready! Press Ctrl+C to exit.")

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c
	log.Println("Shutting down all controllers...")
}
