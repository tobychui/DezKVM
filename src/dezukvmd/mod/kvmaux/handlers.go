package kvmaux

import (
	"encoding/json"
	"log"
	"net/http"
)

// Handler for switching USB to KVM side
func (c *AuxMcu) HandleSwitchUSBToKVM(w http.ResponseWriter, r *http.Request) {
	if err := c.SwitchUSBToKVM(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Println("Switched USB mass storage to KVM side")
	w.WriteHeader(http.StatusOK)
}

// Handler for switching USB to Remote side
func (c *AuxMcu) HandleSwitchUSBToRemote(w http.ResponseWriter, r *http.Request) {
	if err := c.SwitchUSBToRemote(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Println("Switched USB mass storage to remote side")
	w.WriteHeader(http.StatusOK)
}

// Handler for pressing the power button
func (c *AuxMcu) HandlePressPowerButton(w http.ResponseWriter, r *http.Request) {
	if err := c.PressPowerButton(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// Handler for releasing the power button
func (c *AuxMcu) HandleReleasePowerButton(w http.ResponseWriter, r *http.Request) {
	if err := c.ReleasePowerButton(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// Handler for pressing the reset button
func (c *AuxMcu) HandlePressResetButton(w http.ResponseWriter, r *http.Request) {
	if err := c.PressResetButton(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// Handler for releasing the reset button
func (c *AuxMcu) HandleReleaseResetButton(w http.ResponseWriter, r *http.Request) {
	if err := c.ReleaseResetButton(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

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
