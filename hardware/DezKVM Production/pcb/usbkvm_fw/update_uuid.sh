#!/bin/bash

# Script to generate a new UUIDv4 and update the DEVICE_UUID in kvm_uuid_service.ino
# Usage: ./update_uuid.sh

# Get the directory where the script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TARGET_FILE="$SCRIPT_DIR/kvm_uuid_service.ino"

# Check if kvm_uuid_service.ino exists
if [ ! -f "$TARGET_FILE" ]; then
    echo "Error: kvm_uuid_service.ino not found in $SCRIPT_DIR"
    exit 1
fi

# Generate a new UUIDv4
# Try different methods depending on what's available
if command -v uuidgen &> /dev/null; then
    # Use uuidgen (available on most Linux/Unix systems and Git Bash on Windows)
    NEW_UUID=$(uuidgen | tr '[:upper:]' '[:lower:]')
elif command -v python3 &> /dev/null; then
    # Use python3 as fallback
    NEW_UUID=$(python3 -c "import uuid; print(str(uuid.uuid4()))")
elif command -v python &> /dev/null; then
    # Use python as fallback
    NEW_UUID=$(python -c "import uuid; print(str(uuid.uuid4()))")
else
    echo "Error: No UUID generation tool found (tried uuidgen, python3, python)"
    exit 1
fi

# Force first two chars: '1' = device_type usbkvm (0x01), '1' = Revision: production form factor v1 (0x01)
NEW_UUID="11${NEW_UUID:2}"

# Validate UUID format
if [[ ! $NEW_UUID =~ ^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ ]]; then
    echo "Error: Generated UUID '$NEW_UUID' is not a valid UUIDv4"
    exit 1
fi

echo "Generated new UUID: $NEW_UUID"

# Backup the original file
cp "$TARGET_FILE" "$TARGET_FILE.bak"
echo "Created backup: kvm_uuid_service.ino.bak"

# Replace the UUID in the file
# This searches for the line with DEVICE_UUID and replaces the UUID value
sed -i "s/\(#define DEVICE_UUID \"\)[^\"]*\(\"\)/\1$NEW_UUID\2/" "$TARGET_FILE"

# Verify the replacement was successful
if grep -q "$NEW_UUID" "$TARGET_FILE"; then
    echo "Successfully updated DEVICE_UUID to: $NEW_UUID"
    rm "$TARGET_FILE.bak"
else
    echo "Error: Failed to update UUID, restoring backup"
    mv "$TARGET_FILE.bak" "$TARGET_FILE"
    exit 1
fi
