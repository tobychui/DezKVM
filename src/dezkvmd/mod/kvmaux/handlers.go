package kvmaux

import (
	"encoding/json"
	"net/http"
)

// Handler for getting the UUID
func (c *AuxMcu) HandleGetUUID(w http.ResponseWriter, r *http.Request) {
	uuid, err := c.GetUUID()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"uuid": uuid})
}

// Handler for getting the USB mass storage side
func (c *AuxMcu) HandleGetUSBMassStorageSide(w http.ResponseWriter, r *http.Request) {
	side := c.GetUSBMassStorageSide()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"usb_mass_storage_side": side})
}
