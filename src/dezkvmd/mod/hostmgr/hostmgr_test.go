package hostmgr

import (
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseProcStat(t *testing.T) {
	// A command name with spaces and parentheses must not shift the fields
	line := "1234 (my (odd) name) S 1 1234 1234 0 -1 4194560 100 0 0 0 250 50 0 0 20 0 3 0 4567 1000000 300 18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0"
	st, err := parseProcStat([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	if st.name != "my (odd) name" || st.state != "S" || st.ppid != 1 {
		t.Errorf("name/state/ppid = %q %q %d", st.name, st.state, st.ppid)
	}
	if st.ticks != 300 || st.threads != 3 || st.startTick != 4567 || st.rssPages != 300 {
		t.Errorf("ticks %d threads %d start %d rss %d", st.ticks, st.threads, st.startTick, st.rssPages)
	}
	if st.flags&pfKthread != 0 {
		t.Error("user process flagged as kernel thread")
	}

	kthread := "2 (kthreadd) S 0 0 0 0 -1 2129984 0 0 0 0 0 0 0 0 20 0 1 0 2 0 0 18446744073709551615 0 0 0 0 0 0 0 2147483647 0 0 0 0 0 0 0 0 0 0 0"
	st, err = parseProcStat([]byte(kthread))
	if err != nil {
		t.Fatal(err)
	}
	if st.flags&pfKthread == 0 {
		t.Error("kthreadd not flagged as kernel thread")
	}

	if _, err := parseProcStat([]byte("garbage")); err == nil {
		t.Error("expected an error for malformed input")
	}
}

func TestParseNetDev(t *testing.T) {
	data := `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:  123456     100    0    0    0     0          0         0   123456     100    0    0    0     0       0          0
  eth0: 9876543   20000    0    0    0     0          0         0  1234567   15000    0    0    0     0       0          0
`
	got := parseNetDev([]byte(data))
	if got["eth0"] != (ifaceCounters{9876543, 1234567}) || got["lo"].rx != 123456 || len(got) != 2 {
		t.Fatalf("unexpected counters: %+v", got)
	}
}

func TestProcessListMeasuresCPU(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // keep one core busy
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
		}
	}()
	defer func() { close(stop); wg.Wait() }()

	sampler := &procSampler{}
	if _, err := sampler.list(); err != nil { // takes the baseline
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	procs, err := sampler.list()
	if err != nil {
		t.Fatal(err)
	}
	var self *Process
	for i := range procs {
		if procs[i].PID == os.Getpid() {
			self = &procs[i]
		}
	}
	if self == nil {
		t.Fatal("own process missing from the list")
	}
	// One busy core out of NumCPU
	want := 100 / float64(runtime.NumCPU())
	if self.CPU < want*0.5 || self.CPU > 100 {
		t.Errorf("own CPU %.1f%%, expected about %.1f%%", self.CPU, want)
	}
	if self.RSS == 0 || self.User == "" || self.Kernel || self.Started == 0 || !strings.Contains(self.Cmd, "hostmgr") {
		t.Errorf("unexpected own entry: %+v", *self)
	}
}

// newTestManager returns a manager whose power commands are recorded, never run.
func newTestManager(password string) (*Manager, *[]string, *sync.Mutex) {
	m := New(Options{ValidatePassword: func(p string) (bool, error) { return p == password, nil }})
	var ran []string
	var mu sync.Mutex
	m.runCommand = func(name string, args ...string) ([]byte, error) {
		mu.Lock()
		ran = append(ran, name+" "+strings.Join(args, " "))
		mu.Unlock()
		return nil, nil
	}
	m.isRoot = func() bool { return true }
	m.powerDelay = 0
	return m, &ran, &mu
}

func postPower(m *Manager, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/host/power", strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	m.HandlePower(rec, req)
	return rec
}

func TestPowerRequiresPassword(t *testing.T) {
	m, ran, mu := newTestManager("s3cret")

	cases := []struct {
		name, contentType, body string
		want                    int
	}{
		{"wrong password", "application/json", `{"action":"reboot","password":"nope"}`, http.StatusForbidden},
		{"empty password", "application/json", `{"action":"shutdown","password":""}`, http.StatusForbidden},
		{"form post", "application/x-www-form-urlencoded", `action=reboot&password=s3cret`, http.StatusUnsupportedMediaType},
		{"unknown action", "application/json", `{"action":"format","password":"s3cret"}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		if rec := postPower(m, c.contentType, c.body); rec.Code != c.want {
			t.Errorf("%s: status %d, want %d", c.name, rec.Code, c.want)
		}
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if len(*ran) != 0 {
		t.Fatalf("a rejected request ran %v", *ran)
	}
	mu.Unlock()

	if rec := postPower(m, "application/json", `{"action":"reboot","password":"s3cret"}`); rec.Code != http.StatusOK {
		t.Fatalf("correct password: status %d: %s", rec.Code, rec.Body.String())
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(*ran)
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*ran) != 1 || !strings.Contains((*ran)[0], "reboot") && !strings.Contains((*ran)[0], "-r now") {
		t.Fatalf("expected one reboot command, ran %v", *ran)
	}
}

func TestPowerNeedsRoot(t *testing.T) {
	m, ran, _ := newTestManager("pw")
	m.isRoot = func() bool { return false }
	if rec := postPower(m, "application/json", `{"action":"shutdown","password":"pw"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403 without root", rec.Code)
	}
	if len(*ran) != 0 {
		t.Fatalf("ran %v without root", *ran)
	}
}

func TestSummary(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	m := New(Options{})
	s := m.summary()
	if s.RAMTotal == 0 || s.RAMUsed == 0 || s.RAMUsed > s.RAMTotal {
		t.Errorf("RAM %d / %d", s.RAMUsed, s.RAMTotal)
	}
	if s.Uptime <= 0 || s.Kernel == "" || s.Threads == 0 || s.Processes == 0 {
		t.Errorf("summary incomplete: %+v", s)
	}
	if s.Disk == nil || s.Disk.Total == 0 || s.Disk.Used > s.Disk.Total {
		t.Errorf("disk %+v", s.Disk)
	}
}
