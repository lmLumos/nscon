#!/bin/bash

# Cleanup Multi-Controller Nintendo Switch Pro Controller USB Gadgets
# Usage: ./cleanup_procon_gadgets.sh

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "Please run as root (use sudo)"
    exit 1
fi

echo "Cleaning up Nintendo Switch Pro Controller USB Gadgets..."

cd /sys/kernel/config/usb_gadget/

# Function to clean up a gadget
cleanup_gadget() {
    local gadget_name=$1
    if [ -d "$gadget_name" ]; then
        echo "Removing gadget: $gadget_name"
        cd "$gadget_name"
        
        # Deactivate gadget first
        echo "" > UDC 2>/dev/null || true
        
        # Wait a moment for deactivation
        sleep 1
        
        # Remove all function symlinks from config
        rm -f configs/c.1/hid.* 2>/dev/null || true
        
        # Remove HID functions
        for hid_func in functions/hid.*; do
            if [ -d "$hid_func" ]; then
                rmdir "$hid_func" 2>/dev/null || true
            fi
        done
        
        # Remove config directories
        rmdir configs/c.1/strings/0x409 2>/dev/null || true
        rmdir configs/c.1 2>/dev/null || true
        rmdir strings/0x409 2>/dev/null || true
        
        cd ..
        rmdir "$gadget_name" 2>/dev/null || true
        echo "  Cleaned up $gadget_name"
    fi
}

# Clean up multi-controller gadget
cleanup_gadget "multi_procon"

# Clean up any individual controller gadgets  
for i in {0..7}; do
    cleanup_gadget "procon$i"
done

# Clean up original single gadget if it exists
cleanup_gadget "procon"

echo ""

# Check if any hidg devices still exist
if ls /dev/hidg* 1> /dev/null 2>&1; then
    echo "⚠️  Some hidg devices may still exist:"
    ls -la /dev/hidg* 2>/dev/null || true
    echo ""
    echo "If devices persist, try:"
    echo "1. Disconnect and reconnect the USB cable"
    echo "2. Reboot the Raspberry Pi"
    echo "3. Check 'lsmod | grep dwc2' and 'lsmod | grep libcomposite'"
else
    echo "✅ All hidg devices have been removed successfully!"
fi

echo ""
echo "🧹 USB gadget cleanup complete!"
