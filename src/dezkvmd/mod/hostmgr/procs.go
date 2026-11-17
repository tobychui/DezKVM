package hostmgr

import (
	"bytes"
	"errors"
	"os"
	"os/user"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Process is one running process, read from /proc.
type Process struct {
	PID     int     `json:"pid"`
	PPID    int     `json:"ppid"`
	User    string  `json:"user"`
	Name    string  `json:"name"`
	Cmd     string  `json:"cmd"`
	State   string  `json:"state"`
	CPU     float64 `json:"cpu"` // percent of the whole CPU (all cores = 100)
	Mem     float64 `json:"mem"` // percent of RAM
	RSS     uint64  `json:"rss"` // bytes
	Threads int     `json:"threads"`
	Started int64   `json:"started"` // unix time
	Kernel  bool    `json:"kernel"`  // kernel thread
}

// userHZ is the clock tick rate of /proc/[pid]/stat times. It is 100 on
// every mainstream Linux architecture (USER_HZ, fixed by the kernel ABI).
const userHZ = 100

const pfKthread = 0x00200000 // PF_KTHREAD in /proc/[pid]/stat flags

const maxCmdLen = 400

// procStat is the part of /proc/[pid]/stat the process list needs.
type procStat struct {
	name      string
	state     string
	ppid      int
	flags     uint64
	ticks     uint64 // utime + stime
	threads   int
	startTick uint64 // since boot
	rssPages  uint64
}

// parseProcStat parses /proc/[pid]/stat. The command name is in
// parentheses and may itself contain spaces or parentheses, so the fields
// are split after the last ')'.
func parseProcStat(data []byte) (procStat, error) {
	open := bytes.IndexByte(data, '(')
	close := bytes.LastIndexByte(data, ')')
	if open < 0 || close < open {
		return procStat{}, errors.New("malformed stat")
	}
	fields := strings.Fields(string(data[close+1:]))
	// fields[0] is field 3 (state) of proc(5)
	if len(fields) < 22 {
		return procStat{}, errors.New("short stat")
	}
	num := func(i int) uint64 {
		v, _ := strconv.ParseUint(fields[i], 10, 64)
		return v
	}
	ppid, _ := strconv.Atoi(fields[1])
	threads, _ := strconv.Atoi(fields[17])
	return procStat{
		name:      string(data[open+1 : close]),
		state:     fields[0],
		ppid:      ppid,
		flags:     num(6),
		ticks:     num(11) + num(12),
		threads:   threads,
		startTick: num(19),
		rssPages:  num(21),
	}, nil
}

// procSampler lists processes with their CPU usage over the time since the
// previous call (like top).
type procSampler struct {
	mu        sync.Mutex
	prev      map[int]uint64 // pid -> ticks
	prevAt    time.Time
	userNames map[uint32]string
}

// baselineMaxAge: an older baseline is retaken, so the CPU column shows
// current usage rather than an average over minutes.
const baselineMaxAge = 15 * time.Second

func (p *procSampler) list() ([]Process, error) {
	if runtime.GOOS != "linux" {
		return nil, errors.New("process list is only available on Linux")
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.prev == nil || time.Since(p.prevAt) > baselineMaxAge {
		p.prev, p.prevAt = readAllTicks(), time.Now()
		time.Sleep(500 * time.Millisecond)
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	now := time.Now()
	elapsed := now.Sub(p.prevAt).Seconds()
	capacity := elapsed * userHZ * float64(runtime.NumCPU())
	bootTime := readBootTime()
	memTotal := readMemTotal()
	pageSize := uint64(os.Getpagesize())

	procs := make([]Process, 0, len(entries))
	ticks := make(map[int]uint64, len(entries))
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		dir := "/proc/" + entry.Name()
		data, err := os.ReadFile(dir + "/stat")
		if err != nil {
			continue // exited meanwhile
		}
		st, err := parseProcStat(data)
		if err != nil {
			continue
		}
		ticks[pid] = st.ticks

		proc := Process{
			PID:     pid,
			PPID:    st.ppid,
			Name:    st.name,
			State:   st.state,
			Threads: st.threads,
			RSS:     st.rssPages * pageSize,
			Kernel:  st.flags&pfKthread != 0,
		}
		if bootTime > 0 {
			proc.Started = bootTime + int64(st.startTick/userHZ)
		}
		if prevTicks, ok := p.prev[pid]; ok && st.ticks >= prevTicks && capacity > 0 {
			proc.CPU = round1(float64(st.ticks-prevTicks) / capacity * 100)
		} else if !ok && capacity > 0 && proc.Started >= p.prevAt.Unix() {
			proc.CPU = round1(float64(st.ticks) / capacity * 100) // started since the baseline
		}
		if memTotal > 0 {
			proc.Mem = round1(float64(proc.RSS) / float64(memTotal) * 100)
		}
		if info, err := os.Stat(dir); err == nil {
			if sys, ok := info.Sys().(*syscall.Stat_t); ok {
				proc.User = p.userName(sys.Uid)
			}
		}
		if !proc.Kernel {
			proc.Cmd = readCmdline(dir)
		}
		if proc.Cmd == "" {
			proc.Cmd = "[" + st.name + "]"
		}
		procs = append(procs, proc)
	}

	p.prev, p.prevAt = ticks, now
	return procs, nil
}

func (p *procSampler) userName(uid uint32) string {
	if name, ok := p.userNames[uid]; ok {
		return name
	}
	name := strconv.FormatUint(uint64(uid), 10)
	if u, err := user.LookupId(name); err == nil {
		name = u.Username
	}
	if p.userNames == nil {
		p.userNames = map[uint32]string{}
	}
	p.userNames[uid] = name
	return name
}

// readAllTicks returns the CPU ticks of every process.
func readAllTicks() map[int]uint64 {
	out := map[int]uint64{}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return out
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			continue
		}
		if st, err := parseProcStat(data); err == nil {
			out[pid] = st.ticks
		}
	}
	return out
}

func readCmdline(dir string) string {
	data, err := os.ReadFile(dir + "/cmdline")
	if err != nil {
		return ""
	}
	cmd := strings.TrimSpace(strings.ReplaceAll(string(bytes.TrimRight(data, "\x00")), "\x00", " "))
	if len(cmd) > maxCmdLen {
		cmd = cmd[:maxCmdLen] + "…"
	}
	return cmd
}

// readBootTime returns the boot time (unix seconds) from /proc/stat.
func readBootTime() int64 {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, found := strings.CutPrefix(line, "btime "); found {
			bt, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			return bt
		}
	}
	return 0
}

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}
