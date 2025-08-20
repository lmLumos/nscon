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

// ControllerManager manages multiple Nintendo Switch controllers with separate USB gadgets
type ControllerManager struct {
	controllers map[int]*nscon.Controller
	devices     map[int]string
	hidgDevices map[int]string
	mutex       sync.RWMutex
	logLevel    int
	running     bool
}

// NewControllerManager creates a new multi-controller manager
func NewControllerManager(logLevel int) *ControllerManager {
	return &ControllerManager{
		controllers: make(map[int]*nscon.Controller),
		devices:     make(map[int]string),
		hidgDevices: make(map[int]string),
		logLevel:    logLevel,
		running:     true,
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

	// Verify hidg device exists
	if _, err := os.Stat(hidgDevice); os.IsNotExist(err) {
		return fmt.Errorf("hidg device %s does not exist", hidgDevice)
	}

	// Verify input device exists
	if _, err := os.Stat(inputDevice); os.IsNotExist(err) {
		return fmt.Errorf("input device %s does not exist", inputDevice)
	}

	// Create new Nintendo Switch controller
	controller := nscon.NewController(hidgDevice)
	controller.LogLevel = cm.logLevel

	// Connect to the Nintendo Switch
	err := controller.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect controller %d to %s: %v", playerNum, hidgDevice, err)
	}

	cm.controllers[playerNum] = controller
	cm.devices[playerNum] = inputDevice
	cm.hidgDevices[playerNum] = hidgDevice

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
		delete(cm.hidgDevices, playerNum)
		log.Printf("Controller %d disconnected", playerNum)
	}
}

// Close closes all controllers
func (cm *ControllerManager) Close() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	cm.running = false

	for playerNum, controller := range cm.controllers {
		controller.Close()
		log.Printf("Controller %d closed", playerNum)
	}

	cm.controllers = make(map[int]*nscon.Controller)
	cm.devices = make(map[int]string)
	cm.hidgDevices = make(map[int]string)
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

	for cm.running {
		// Check if controller still exists
		cm.mutex.RLock()
		_, exists := cm.controllers[playerNum]
		cm.mutex.RUnlock()

		if !exists {
			log.Printf("Controller %d: Stopping input reader", playerNum)
			return
		}

		// Set read timeout to allow for clean shutdown
		file.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

		n, err := file.Read(buffer)
		if err != nil {
			// Check if it's a timeout error (expected for clean shutdown)
			if strings.Contains(err.Error(), "timeout") {
				continue
			}
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

		if cm.logLevel > 2 {
			log.Printf("Controller %d: Button event - Code: %d, Pressed: %t", playerNum, code, pressed)
		}

	case EV_ABS:
		// Debug output to see raw values
		if cm.logLevel > 2 {
			log.Printf("Controller %d: Axis event - Code: %d, Raw Value: %d", playerNum, code, value)
		}

		// Normalize axis values for different controller types
		var normalizedValue float64

		// Handle different controller ranges
		if value >= 0 && value <= 255 {
			// 8-bit unsigned range (0-255)
			normalizedValue = (float64(value) - 127.5) / 127.5
		} else if value >= -32768 && value <= 32767 {
			// 16-bit signed range (-32768 to 32767)
			normalizedValue = float64(value) / 32767.0
		} else if value >= 0 && value <= 4095 {
			// 12-bit unsigned range
			normalizedValue = (float64(value) - 2048.0) / 2048.0
		} else {
			// Fallback to 8-bit unsigned
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
func findInputDevices() map[string]string {
	devices := make(map[string]string)

	// Check event devices
	for i := 0; i < 20; i++ {
		path := fmt.Sprintf("/dev/input/event%d", i)
		if _, err := os.Stat(path); err == nil {
			// Try to get device name from sysfs
			namePath := fmt.Sprintf("/sys/class/input/event%d/device/name", i)
			if nameBytes, err := os.ReadFile(namePath); err == nil {
				name := strings.TrimSpace(string(nameBytes))
				// Look for common controller names
				if isControllerDevice(name) {
					key := fmt.Sprintf("%s%d", name, i)
					devices[key] = path
					log.Printf("Found controller: %s at %s", key, path)
				}
			}
		}
	}

	return devices
}

// isControllerDevice checks if a device name indicates a game controller
func isControllerDevice(name string) bool {
	controllerNames := []string{
		"Wireless Controller",
		"Xbox",
		"PlayStation",
		"PS3",
		"PS4",
		"PS5",
		"DualShock",
		"DualSense",
		"Pro Controller",
		"Joy-Con",
		"8Bitdo",
		"Nintendo",
		"Gamepad",
	}

	nameLower := strings.ToLower(name)
	for _, controllerName := range controllerNames {
		if nameLower== strings.ToLower(controllerName) {
			return true
		}
	}
	return false
}

// findHidgDevices finds available hidg devices
func findHidgDevices() []string {
	var devices []string
	for i := 0; i < 8; i++ {
		path := fmt.Sprintf("/dev/hidg%d", i)
		if _, err := os.Stat(path); err == nil {
			devices = append(devices, path)
		}
	}
	return devices
}

// setupControllerMapping provides interactive controller setup
func setupControllerMapping(manager *ControllerManager) {
	fmt.Println("\n=== Controller Mapping Setup ===")
	
	inputDevices := findInputDevices()
	hidgDevices := findHidgDevices()

	if len(inputDevices) == 0 {
		fmt.Println("❌ No input controllers found!")
		fmt.Println("Make sure your controllers are connected and recognized by the system.")
		fmt.Println("Use 'sudo evtest' to verify controller detection.")
		return
	}

	if len(hidgDevices) == 0 {
		fmt.Println("❌ No hidg devices found!")
		fmt.Println("Make sure USB gadgets are set up correctly.")
		fmt.Println("Run the setup script: sudo ./setup_separate_procon_gadgets.sh")
		return
	}

	fmt.Printf("Found %d input device(s) and %d hidg device(s)\n", len(inputDevices), len(hidgDevices))
	
	fmt.Println("\nAvailable input controllers:")
	inputList := make([]string, 0, len(inputDevices))
	for name, path := range inputDevices {
		fmt.Printf("  %s -> %s\n", name, path)
		inputList = append(inputList, path)
	}

	fmt.Println("\nAvailable hidg devices:")
	for i, device := range hidgDevices {
		fmt.Printf("  %d: %s\n", i+1, device)
	}

	// Interactive mapping
	scanner := bufio.NewScanner(os.Stdin)
	playerNum := 1

	for len(inputList) > 0 && playerNum <= len(hidgDevices) {
		fmt.Printf("\nController %d setup:\n", playerNum)
		
		// Select input device
		fmt.Printf("Select input device (1-%d, or 'skip'): ", len(inputList))
		scanner.Scan()
		choice := strings.TrimSpace(scanner.Text())
		
		if choice == "skip" {
			break
		}

		inputIndex, err := strconv.Atoi(choice)
		if err != nil || inputIndex < 1 || inputIndex > len(inputList) {
			fmt.Println("Invalid selection!")
			continue
		}

		inputDevice := inputList[inputIndex-1]
		hidgDevice := hidgDevices[playerNum-1]

		fmt.Printf("Mapping: Player %d = %s -> %s\n", playerNum, inputDevice, hidgDevice)
		
		err = manager.AddController(playerNum, hidgDevice, inputDevice)
		if err != nil {
			fmt.Printf("❌ Failed to add controller %d: %v\n", playerNum, err)
			continue
		}

		// Remove selected device from list
		inputList = append(inputList[:inputIndex-1], inputList[inputIndex:]...)
		playerNum++
	}

	if len(manager.ListControllers()) == 0 {
		fmt.Println("❌ No controllers were successfully configured!")
		return
	}

	fmt.Printf("\n✅ Successfully configured %d controller(s)!\n", len(manager.ListControllers()))
}

func printUsage() {
	fmt.Println("Multi-Controller Nintendo Switch Controller Simulator")
	fmt.Println("Supports separate USB gadgets for true multi-controller functionality")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("  sudo go run improved_multi_controller.go [options]")
	fmt.Println("")
	fmt.Println("Options:")
	fmt.Println("  --auto          Auto-detect and map controllers")
	fmt.Println("  --interactive   Interactive controller setup (default)")
	fmt.Println("  --manual        Manual controller configuration")
	fmt.Println("  --debug         Enable debug logging")
	fmt.Println("  --help, -h      Show this help")
	fmt.Println("")
	fmt.Println("Prerequisites:")
	fmt.Println("  1. Run sudo ./setup_separate_procon_gadgets.sh [num_controllers]")
	fmt.Println("  2. Connect your Bluetooth/USB controllers")
	fmt.Println("  3. Verify with 'sudo evtest' that controllers are detected")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("  sudo go run improved_multi_controller.go --interactive")
	fmt.Println("  sudo go run improved_multi_controller.go --debug --auto")
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
		case "--interactive":
			// Interactive is default, no need to set flag
		case "--manual":
			manualMode = true
			autoMode = false
		case "--debug":
			debugMode = true
		}
	}

	logLevel := 1
	if debugMode {
		logLevel = 3
		log.Println("Debug mode enabled")
	}

	// Create controller manager
	manager := NewControllerManager(logLevel)
	defer manager.Close()

	fmt.Println("🎮 Multi-Controller Nintendo Switch Simulator")
	fmt.Println("Using separate USB gadgets for true multi-controller support")
	fmt.Println()

	if autoMode {
		// Auto-detect mode
		fmt.Println("🔍 Auto-detecting controllers...")
		
		inputDevices := findInputDevices()
		hidgDevices := findHidgDevices()

		if len(inputDevices) == 0 {
			fmt.Println("❌ No controllers found!")
			return
		}

		if len(hidgDevices) == 0 {
			fmt.Println("❌ No hidg devices found! Run setup script first.")
			return
		}

		// Auto-map controllers
		playerNum := 1
		for _, inputDevice := range inputDevices {
			if playerNum > len(hidgDevices) {
				break
			}
			
			hidgDevice := hidgDevices[playerNum-1]
			err := manager.AddController(playerNum, hidgDevice, inputDevice)
			if err != nil {
				log.Printf("Failed to add controller %d: %v", playerNum, err)
			} else {
				playerNum++
			}
		}

	} else if manualMode {
		// Manual mode
		fmt.Println("📝 Manual controller setup")
		fmt.Println("Enter controller configurations (player:input_device:hidg_device)")
		fmt.Println("Example: 1:/dev/input/event2:/dev/hidg0")
		fmt.Println("Enter 'done' when finished")

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
				fmt.Println("Invalid format. Use: player:input_device:hidg_device")
				continue
			}

			playerNum, err := strconv.Atoi(parts[0])
			if err != nil {
				fmt.Printf("Invalid player number: %s\n", parts[0])
				continue
			}

			inputDevice := parts[1]
			hidgDevice := parts[2]

			err = manager.AddController(playerNum, hidgDevice, inputDevice)
			if err != nil {
				fmt.Printf("Failed to add controller: %v\n", err)
			}
		}

	} else if !autoMode && !manualMode {
		// Interactive mode (default)
		setupControllerMapping(manager)
	}

	// Show active controllers
	controllers := manager.ListControllers()
	if len(controllers) == 0 {
		fmt.Println("❌ No controllers active. Exiting...")
		return
	}

	fmt.Printf("✅ Active controllers: %v\n", controllers)
	fmt.Println("🔌 Connect your Nintendo Switch via USB cable")
	fmt.Println("🎮 Controllers are ready! Press Ctrl+C to exit.")

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c
	fmt.Println("\n🛑 Shutting down all controllers...")
}
