#!/bin/bash

# USB Hub Emulation for Multiple Nintendo Switch Controllers
# This creates a composite device that emulates a USB hub with multiple controllers
# Usage: ./setup_hub_emulation.sh [number_of_controllers]

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "Please run as root (use sudo)"
    exit 1
fi

NUM_CONTROLLERS=${1:-2}

if [ "$NUM_CONTROLLERS" -lt 1 ] || [ "$NUM_CONTROLLERS" -gt 4 ]; then
    echo "Error: 1-4 controllers supported"
    echo "Usage: $0 [number_of_controllers]"
    exit 1
fi

echo "Setting up USB Hub emulation with $NUM_CONTROLLERS Nintendo Switch Pro Controllers..."

# Clean up existing gadgets
echo "Cleaning up existing gadgets..."
cd /sys/kernel/config/usb_gadget/
for existing in hub_procon*; do
    if [ -d "$existing" ]; then
        echo "Removing existing gadget: $existing"
        cd "$existing"
        echo "" > UDC 2>/dev/null || true
        rm -f configs/c.1/* 2>/dev/null || true
        rmdir functions/* 2>/dev/null || true
        rmdir configs/c.1/strings/* 2>/dev/null || true
        rmdir configs/c.1 2>/dev/null || true
        rmdir strings/* 2>/dev/null || true
        cd ..
        rmdir "$existing" 2>/dev/null || true
    fi
done

sleep 2

# Check UDC availability
UDC_NAME=$(ls /sys/class/udc/ | head -1)
if [ -z "$UDC_NAME" ]; then
    echo "Error: No USB Device Controllers found!"
    exit 1
fi

echo "Using UDC: $UDC_NAME"

# Create hub emulation gadget
GADGET_NAME="hub_procon"
mkdir -p $GADGET_NAME
cd $GADGET_NAME

# Set USB device parameters for a hub-like device
echo 0x1d6b > idVendor          # Linux Foundation (for composite devices)
echo 0x0104 > idProduct         # Composite device
echo 0x0100 > bcdDevice
echo 0x0200 > bcdUSB

# Use composite device class
echo 0x09 > bDeviceClass        # Hub class
echo 0x00 > bDeviceSubClass     
echo 0x00 > bDeviceProtocol

# Set device strings
mkdir -p strings/0x409
echo "Nintendo Controller Hub" > strings/0x409/serialnumber
echo "Nintendo Co., Ltd." > strings/0x409/manufacturer
echo "Switch Pro Controller Hub" > strings/0x409/product

# Configuration setup
mkdir -p configs/c.1/strings/0x409
echo "Nintendo Switch Controllers Hub" > configs/c.1/strings/0x409/configuration
echo $((500 * NUM_CONTROLLERS)) > configs/c.1/MaxPower
echo 0xa0 > configs/c.1/bmAttributes

# Nintendo Switch Pro Controller HID Report Descriptor
HID_REPORT_DESC="050115000904A1018530050105091901290A150025017501950A5500650081020509190B290E150025017501950481027501950281030B01000100A1000B300001000B310001000B320001000B35000100150027FFFF0000751095048102C00B39000100150025073500463B0165147504950181020509190F2912150025017501950481027508953481030600FF852109017508953F8103858109027508953F8103850109037508953F9183851009047508953F9183858009057508953F9183858209067508953F9183C0"

# Create multiple HID functions with different interface numbers
for i in $(seq 0 $((NUM_CONTROLLERS-1))); do
    echo "Creating HID function for controller $((i+1))"
    
    mkdir -p functions/hid.controller$i
    echo $i > functions/hid.controller$i/protocol        # Use controller number as protocol
    echo 0 > functions/hid.controller$i/subclass
    echo 64 > functions/hid.controller$i/report_length
    echo "$HID_REPORT_DESC" | xxd -r -ps > functions/hid.controller$i/report_desc
    
    # Link to configuration with specific interface association
    ln -s functions/hid.controller$i configs/c.1/
done

# Activate the gadget
echo "$UDC_NAME" > UDC

echo ""
echo "✅ Hub emulation gadget created!"

# Wait for devices to appear
sleep 3

echo ""
echo "Checking for hidg devices..."
for i in $(seq 0 $((NUM_CONTROLLERS-1))); do
    if [ -e "/dev/hidg$i" ]; then
        chmod 666 "/dev/hidg$i"
        echo "  ✅ Controller $((i+1)): /dev/hidg$i (ready)"
    else
        echo "  ❌ Controller $((i+1)): /dev/hidg$i (not found)"
    fi
done

# Create a service script for the hub controller manager
echo ""
echo "Creating hub controller management script..."

cat > /home/pi/hub_controller_manager.py << 'EOF'
#!/usr/bin/env python3
"""
Hub Controller Manager for Nintendo Switch
Manages multiple HID devices that appear as separate controllers through USB hub emulation
"""

import os
import sys
import time
import threading
import signal
import logging

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s'
)

class HubControllerManager:
    def __init__(self, num_controllers=2):
        self.num_controllers = num_controllers
        self.devices = {}
        self.running = True
        
        # Find available hidg devices
        for i in range(num_controllers):
            device_path = f"/dev/hidg{i}"
            if os.path.exists(device_path):
                self.devices[i] = device_path
                logging.info(f"Found controller {i+1} at {device_path}")
            else:
                logging.warning(f"Controller {i+1} device {device_path} not found")
    
    def send_to_controller(self, controller_id, data):
        """Send data to specific controller"""
        if controller_id not in self.devices:
            return False
            
        try:
            with open(self.devices[controller_id], 'wb') as f:
                f.write(data)
            return True
        except Exception as e:
            logging.error(f"Failed to send to controller {controller_id}: {e}")
            return False
    
    def broadcast_to_all(self, data):
        """Send data to all controllers"""
        for controller_id in self.devices:
            self.send_to_controller(controller_id, data)
    
    def monitor_controllers(self):
        """Monitor controller status"""
        while self.running:
            # Check if devices are still available
            for controller_id in list(self.devices.keys()):
                if not os.path.exists(self.devices[controller_id]):
                    logging.warning(f"Controller {controller_id+1} disconnected")
                    del self.devices[controller_id]
            
            # Look for new devices
            for i in range(self.num_controllers):
                if i not in self.devices:
                    device_path = f"/dev/hidg{i}"
                    if os.path.exists(device_path):
                        self.devices[i] = device_path
                        logging.info(f"Controller {i+1} reconnected at {device_path}")
            
            time.sleep(5)
    
    def stop(self):
        """Stop the manager"""
        self.running = False
        logging.info("Hub Controller Manager stopped")

def signal_handler(signum, frame):
    logging.info("Received shutdown signal")
    sys.exit(0)

if __name__ == "__main__":
    signal.signal(signal.SIGINT, signal_handler)
    signal.signal(signal.SIGTERM, signal_handler)
    
    if len(sys.argv) > 1:
        try:
            num_controllers = int(sys.argv[1])
        except ValueError:
            num_controllers = 2
    else:
        num_controllers = 2
    
    manager = HubControllerManager(num_controllers)
    
    # Start monitoring in background
    monitor_thread = threading.Thread(target=manager.monitor_controllers)
    monitor_thread.daemon = True
    monitor_thread.start()
    
    logging.info(f"Hub Controller Manager started with {num_controllers} controllers")
    logging.info(f"Active devices: {list(manager.devices.values())}")
    
    try:
        while True:
            time.sleep(1)
    except KeyboardInterrupt:
        manager.stop()
EOF

chmod +x /home/pi/hub_controller_manager.py

# Create systemd service for automatic startup
cat > /etc/systemd/system/nintendo-hub-controllers.service << EOF
[Unit]
Description=Nintendo Switch Hub Controller Manager
After=network.target

[Service]
Type=simple
User=root
ExecStart=/usr/bin/python3 /home/pi/hub_controller_manager.py $NUM_CONTROLLERS
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

echo ""
echo "📋 Hub emulation setup completed!"
echo ""
echo "📝 Next steps:"
echo "1. Test the setup: python3 /home/pi/hub_controller_manager.py $NUM_CONTROLLERS"
echo "2. Enable auto-start: sudo systemctl enable nintendo-hub-controllers"
echo "3. Start service: sudo systemctl start nintendo-hub-controllers"
echo "4. Run your controller application to use /dev/hidg0, /dev/hidg1, etc."
echo "5. Connect to Nintendo Switch via USB"
echo ""
echo "🔍 Useful commands:"
echo "   Check service status: sudo systemctl status nintendo-hub-controllers"
echo "   View logs: sudo journalctl -u nintendo-hub-controllers -f"
echo "   Check USB enumeration: lsusb"
echo ""
echo "🎮 USB Hub emulation setup complete!"
