#!/bin/bash

echo "This script helps debug audio and USB KVM devices."
echo "Make sure you have the necessary permissions to access audio and USB devices."
echo ""
echo "------------------"
echo "Checking for required tools..."
# Check if 'arecord' (ALSA tool) is installed
if ! command -v arecord &> /dev/null; then
    echo "Warning: 'arecord' (ALSA audio recorder) is not installed. Please install it for audio debugging."
else
    echo "'arecord' is installed."
fi

# Check if 'v4l2-ctl' (Video4Linux2 control tool) is installed
if ! command -v v4l2-ctl &> /dev/null; then
    echo "Warning: 'v4l2-ctl' (Video4Linux2 control tool) is not installed. Please install it for USB video device debugging."
else
    echo "'v4l2-ctl' is installed."
fi

# List all audio devices
echo "------------------"
echo "Listing audio devices"
sudo ./dezukvmd -mode=debug -tool=audio-devices

# List all USB KVM devices
echo "------------------"
echo "Listing USB KVM devices"
sudo ./dezukvmd -mode=debug -tool=list-usbkvm
echo "------------------"
echo "The finalized KVM device group is listed below: "
sudo ./dezukvmd -mode=debug -tool=list-usbkvm-json