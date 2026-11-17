package hostmgr

/*
	hostmgr - maintenance mode backend

	Reports the state of the machine running dezkvmd (CPU, RAM, network,
	disk, temperature, processes, usage history) and restarts or shuts it
	down. Power actions require the admin password to be typed again, on
	top of the logged-in session.

	CPU / RAM sampling and the usage history come from mod/info/usageinfo,
	the CPU model from mod/info/hardwareinfo; this module adds the network
	throughput, the process list (read from /proc, with top-style CPU usage)
	and the HTTP handlers. Linux only for the process list and power actions.
*/

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"imuslab.com/dezkvm/dezkvmd/mod/info/hardwareinfo"
	"imuslab.com/dezkvm/dezkvmd/mod/info/usageinfo"
)

// Options configures the host manager.
type Options struct {
	// ValidatePassword checks the admin password for power actions.
	ValidatePassword func(password string) (bool, error)
}

// Manager serves the maintenance mode APIs.
type Manager struct {
	opt   Options
	net   *netSampler
	procs *procSampler

	startOnce sync.Once

	authMu sync.Mutex // serialises password checks (brute force delay)

	powerMu    sync.Mutex
	powerError string // last failed power action, reported in the summary
	powerAt    time.Time

	// Swappable in tests, so they can never reboot the machine
	runCommand func(name string, args ...string) ([]byte, error)
	isRoot     func() bool
	powerDelay time.Duration
}

// New creates a host manager. Call Start to begin sampling.
func New(opt Options) *Manager {
	return &Manager{
		opt:   opt,
		net:   &netSampler{},
		procs: &procSampler{},
		runCommand: func(name string, args ...string) ([]byte, error) {
			return exec.Command(name, args...).CombinedOutput()
		},
		isRoot:     func() bool { return os.Geteuid() == 0 },
		powerDelay: 1500 * time.Millisecond,
	}
}

// Start begins the background sampling (CPU / RAM / network, usage
// history). Safe to call more than once.
func (m *Manager) Start() {
	m.startOnce.Do(func() {
		usageinfo.SetNetworkCounterSource(totalBits)
		usageinfo.StartBackgroundMonitor()
		hardwareinfo.StartHostInfoCache()
		go m.net.run()
	})
}

/* ------------------------------------------------------------- summary */

// Temperature is one thermal sensor reading.
type Temperature struct {
	Name    string  `json:"name"`
	Celsius float64 `json:"celsius"`
}

// DiskUsage is the usage of one mounted filesystem, in bytes.
type DiskUsage struct {
	Path  string `json:"path"`
	Used  uint64 `json:"used"`
	Total uint64 `json:"total"`
}

// Summary is the live state of the host, polled by maintenance mode.
type Summary struct {
	Hostname   string         `json:"hostname"`
	OS         string         `json:"os"`
	Kernel     string         `json:"kernel"`
	Arch       string         `json:"arch"`
	Model      string         `json:"model"`
	Cores      int            `json:"cores"`
	Threads    int            `json:"threads"`
	Uptime     float64        `json:"uptime"` // seconds
	Load       [3]float64     `json:"load"`
	CPU        float64        `json:"cpu"`       // percent
	CPUReady   bool           `json:"cpu_ready"` // false for ~1 s after start
	RAMUsed    uint64         `json:"ram_used"`
	RAMTotal   uint64         `json:"ram_total"`
	SwapUsed   uint64         `json:"swap_used"`
	SwapTotal  uint64         `json:"swap_total"`
	Disk       *DiskUsage     `json:"disk"`
	Temps      []Temperature  `json:"temps"`
	Net        []NetInterface `json:"net"`
	Processes  int            `json:"processes"`
	Time       int64          `json:"time"`
	PowerError string         `json:"power_error,omitempty"`
}

func (m *Manager) summary() Summary {
	s := Summary{
		Arch:  runtime.GOARCH,
		Time:  time.Now().Unix(),
		Temps: []Temperature{},
		Net:   m.net.interfaces(),
	}
	s.Hostname, _ = os.Hostname()
	s.OS = readOSName()
	s.Kernel = readFirstLine("/proc/sys/kernel/osrelease")
	s.Model = strings.TrimRight(readFirstLine("/proc/device-tree/model"), "\x00 ")
	s.Cores, s.Threads = usageinfo.GetCPUCounts()
	if up := strings.Fields(readFirstLine("/proc/uptime")); len(up) > 0 {
		s.Uptime, _ = strconv.ParseFloat(up[0], 64)
	}
	if load := strings.Fields(readFirstLine("/proc/loadavg")); len(load) >= 3 {
		for i := 0; i < 3; i++ {
			s.Load[i], _ = strconv.ParseFloat(load[i], 64)
		}
	}
	cpu, _, _, _, ready := usageinfo.GetCachedStats()
	s.CPU, s.CPUReady = round1(cpu), ready
	if mem, err := usageinfo.ReadMemInfo(); err == nil {
		s.RAMUsed, s.RAMTotal = mem.Used(), mem.Total
		s.SwapTotal, s.SwapUsed = mem.SwapTotal, mem.SwapTotal-min(mem.SwapFree, mem.SwapTotal)
	} else if used, total := usageinfo.GetNumericRAMUsage(); total > 0 {
		s.RAMUsed, s.RAMTotal = uint64(used), uint64(total)
	}
	s.Disk = diskUsage("/")
	s.Temps = readTemperatures()
	s.Processes = countProcesses()

	m.powerMu.Lock()
	s.PowerError = m.powerError
	m.powerMu.Unlock()
	return s
}

func readFirstLine(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(data), "\n")
	return strings.TrimSpace(line)
}

// readOSName returns PRETTY_NAME from /etc/os-release.
func readOSName() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, found := strings.CutPrefix(line, "PRETTY_NAME="); found {
			return strings.Trim(v, `"'`)
		}
	}
	return runtime.GOOS
}

func readMemTotal() uint64 {
	mem, err := usageinfo.ReadMemInfo()
	if err != nil {
		return 0
	}
	return mem.Total
}

func diskUsage(path string) *DiskUsage {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return nil
	}
	bsize := uint64(st.Bsize)
	total := st.Blocks * bsize
	free := st.Bfree * bsize
	return &DiskUsage{Path: path, Total: total, Used: total - min(free, total)}
}

// readTemperatures reads the kernel thermal zones (SoC sensors on SBCs).
func readTemperatures() []Temperature {
	temps := []Temperature{}
	zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
	sort.Strings(zones)
	for _, zone := range zones {
		milli, err := strconv.ParseFloat(readFirstLine(zone+"/temp"), 64)
		if err != nil || milli <= 0 {
			continue
		}
		name := readFirstLine(zone + "/type")
		if name == "" {
			name = filepath.Base(zone)
		}
		temps = append(temps, Temperature{Name: name, Celsius: round1(milli / 1000)})
	}
	return temps
}

func countProcesses() int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err == nil {
			n++
		}
	}
	return n
}

/* ------------------------------------------------------------ handlers */

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

// HandleSummary returns the live host state (GET).
func (m *Manager) HandleSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, m.summary())
}

// HandleCPUInfo returns the CPU model details (GET), from hardwareinfo.
func (m *Manager) HandleCPUInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	hardwareinfo.CachedGetCPUInfo(w, r)
}

// HandleHistory returns the CPU / RAM / network history (GET ?range=1h|24h).
func (m *Manager) HandleHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rangeName := r.URL.Query().Get("range")
	if rangeName != "24h" {
		rangeName = "1h"
	}
	points, step := usageinfo.GetHistory(rangeName)
	writeJSON(w, map[string]any{
		"range":  rangeName,
		"step":   int(step.Seconds()),
		"points": points,
	})
}

// HandleProcesses returns the running processes (GET).
func (m *Manager) HandleProcesses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	procs, err := m.procs.list()
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotImplemented)
		return
	}
	writeJSON(w, map[string]any{
		"processes": procs,
		"cpus":      runtime.NumCPU(),
	})
}

// HandlePower restarts or shuts down the host (POST JSON
// {"action": "reboot"|"shutdown", "password": "<admin password>"}).
func (m *Manager) HandlePower(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// A JSON body cannot be sent by a plain cross-site form
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	var req struct {
		Action   string `json:"action"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	command, err := powerCommand(req.Action)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !m.checkPassword(req.Password) {
		log.Printf("Maintenance: rejected host %s from %s (wrong password)\n", req.Action, r.RemoteAddr)
		http.Error(w, "Incorrect password", http.StatusForbidden)
		return
	}
	if runtime.GOOS != "linux" {
		http.Error(w, "Power actions are only supported on Linux", http.StatusNotImplemented)
		return
	}
	if !m.isRoot() {
		http.Error(w, "dezkvmd must run as root to "+req.Action+" the host", http.StatusForbidden)
		return
	}

	m.powerMu.Lock()
	m.powerError = ""
	m.powerAt = time.Now()
	m.powerMu.Unlock()

	log.Printf("Maintenance: host %s requested from %s\n", req.Action, r.RemoteAddr)
	writeJSON(w, map[string]string{"status": "ok", "action": req.Action})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Run after the response is on its way: stopping the system also stops
	// dezkvmd (gracefully, via SIGTERM).
	go func() {
		time.Sleep(m.powerDelay)
		out, err := m.runCommand(command[0], command[1:]...)
		if err != nil {
			msg := fmt.Sprintf("%s failed: %v %s", strings.Join(command, " "), err, strings.TrimSpace(string(out)))
			log.Println("Maintenance:", msg)
			m.powerMu.Lock()
			m.powerError = msg
			m.powerMu.Unlock()
		}
	}()
}

// powerCommand maps an action to the command that performs it.
func powerCommand(action string) ([]string, error) {
	_, err := exec.LookPath("systemctl")
	hasSystemctl := err == nil
	switch action {
	case "reboot":
		if hasSystemctl {
			return []string{"systemctl", "reboot"}, nil
		}
		return []string{"shutdown", "-r", "now"}, nil
	case "shutdown":
		if hasSystemctl {
			return []string{"systemctl", "poweroff"}, nil
		}
		return []string{"shutdown", "-h", "now"}, nil
	}
	return nil, errors.New("unknown action, expected reboot or shutdown")
}

// checkPassword validates the admin password. Failures are slowed down and
// serialised so the endpoint cannot be used to guess the password quickly.
func (m *Manager) checkPassword(password string) bool {
	m.authMu.Lock()
	defer m.authMu.Unlock()
	if m.opt.ValidatePassword == nil || password == "" {
		time.Sleep(time.Second)
		return false
	}
	ok, err := m.opt.ValidatePassword(password)
	if err != nil || !ok {
		time.Sleep(time.Second)
		return false
	}
	return true
}
