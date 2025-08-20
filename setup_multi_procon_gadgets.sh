#!/bin/bash

# Multi-Controller Nintendo Switch Pro Controller USB Gadget Setup
# Based on original script by mzyy94
# Usage: ./setup_multi_procon_gadgets.sh [number_of_controllers]
# Example: ./setup_multi_procon_gadgets.sh 4

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "Please run as root (use sudo)"
    exit 1
fi

# Default to 2 controllers if no argument provided
NUM_CONTROLLERS=${1:-2}

# Validate number of controllers
if [ "$NUM_CONTROLLERS" -lt 1 ] || [ "$NUM_CONTROLLERS" -gt 4 ]; then
    echo "Error: Due to Raspberry Pi USB limitations, only 1-4 controllers are supported"
    echo "Usage: $0 [number_of_controllers]"
    exit 1
fi

echo "Setting up $NUM_CONTROLLERS Nintendo Switch Pro Controller USB Gadget(s)..."

# First, clean up any existing gadgets
echo "Cleaning up existing gadgets..."
cd /sys/kernel/config/usb_gadget/
for existing in procon*; do
    if [ -d "$existing" ]; then
        echo "Removing existing gadget: $existing"
        cd "$existing"
        echo "" > UDC 2>/dev/null || true
        rm -f configs/c.1/hid.* 2>/dev/null || true
        rmdir functions/hid.* 2>/dev/null || true
        rmdir configs/c.1/strings/* 2>/dev/null || true
        rmdir configs/c.1 2>/dev/null || true
        rmdir strings/* 2>/dev/null || true
        cd ..
        rmdir "$existing" 2>/dev/null || true
    fi
done

# Wait for cleanup
sleep 2

# Check available UDCs
echo "Available USB Device Controllers:"
ls /sys/class/udc/
UDC_COUNT=$(ls /sys/class/udc/ | wc -l)
echo "Found $UDC_COUNT UDC(s)"

if [ "$UDC_COUNT" -eq 0 ]; then
    echo "Error: No USB Device Controllers found!"
    echo "Make sure dwc2 overlay is enabled in /boot/config.txt:"
    echo "dtoverlay=dwc2"
    exit 1
fi

# Navigate to USB gadget config directory
cd /sys/kernel/config/usb_gadget/

# Create a single gadget with multiple HID functions
GADGET_NAME="multi_procon"
echo "Creating multi-controller gadget: $GADGET_NAME"

mkdir -p $GADGET_NAME
cd $GADGET_NAME

# Set USB device parameters
echo 0x057e > idVendor          # Nintendo Co., Ltd.
echo 0x2009 > idProduct         # Pro Controller
echo 0x0200 > bcdDevice
echo 0x0200 > bcdUSB
echo 0x00 > bDeviceClass
echo 0x00 > bDeviceSubClass  
echo 0x00 > bDeviceProtocol

# Set device strings
mkdir -p strings/0x409
echo "000000000001" > strings/0x409/serialnumber
echo "Nintendo Co., Ltd." > strings/0x409/manufacturer
echo "Pro Controller Hub" > strings/0x409/product

# Set configuration
mkdir -p configs/c.1/strings/0x409
echo "Nintendo Switch Pro Controllers" > configs/c.1/strings/0x409/configuration
echo $((500 * NUM_CONTROLLERS)) > configs/c.1/MaxPower  # Scale power with controller count
echo 0xa0 > configs/c.1/bmAttributes

# HID Report Descriptor
HID_REPORT_DESC="050115000904A1018530050105091901290A150025017501950A5500650081020509190B290E150025017501950481027501950281030B01000100A1000B300001000B310001000B320001000B35000100150027FFFF0000751095048102C00B39000100150025073500463B0165147504950181020509190F2912150025017501950481027508953481030600FF852109017508953F8103858109027508953F8103850109037508953F9183851009047508953F9183858009057508953F9183858209067508953F9183C0"

# Create HID functions for each controller
for i in $(seq 0 $((NUM_CONTROLLERS-1))); do
    echo "  Creating HID function $i"
    
    # Create HID function directory
    mkdir -p functions/hid.usb$i
    echo 0 > functions/hid.usb$i/protocol
    echo 0 > functions/hid.usb$i/subclass  
    echo 64 > functions/hid.usb$i/report_length
    echo "$HID_REPORT_DESC" | xxd -r -ps > functions/hid.usb$i/report_desc
    
    # Link function to configuration
    ln -s functions/hid.usb$i configs/c.1/
done

# Get the first available UDC
UDC_NAME=$(ls /sys/class/udc/ | head -1)
echo "Using UDC: $UDC_NAME"

# Activate the gadget
echo "$UDC_NAME" > UDC

echo ""
echo "✅ Multi-controller gadget created!"

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

echo ""
echo "📝 Next steps:"
echo "1. Connect your Bluetooth controllers to different /dev/input/eventX devices"
echo "2. Run: sudo go run multi_controller.go --manual"
echo "3. Configure each controller manually"
echo "4. Connect Nintendo Switch via USB"

echo ""
echo "🔍 To check controller input devices:"
echo "   sudo evtest"
echo ""
echo "🎮 Multi-controller setup complete!"
