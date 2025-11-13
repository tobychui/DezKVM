#!/bin/bash

# -----------------------------------------------------------------------------
# Script to start dezukvmd in config chip mode for CH9329 configuration.
#
# This script is intended for use with newly soldered CH9329 chips, which have
# a default baudrate of 9600. It allows setting the chip's default baudrate to
# 115200, enabling higher speed for keyboard and mouse virtual machine operation.
#
# The configuration port used here is shared with the USB KVM mode, as defined
# in the config/usbkvm.json file.
#
# Author: toby@aroz.org
#
# License: GPLv3
# -----------------------------------------------------------------------------
sudo ./dezukvmd -mode=cfgchip