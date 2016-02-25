package main

import (
	"io/ioutil"
	"os"
	"os/user"
	"path"
	"strconv"
	"strings"
	"syscall"
)

const (
	// The indices of the values in /proc/<pid>/stat
	statPid = iota
	statComm
	statState
	statPpid
	statPgrp
	statSession
	statTtyNr
	statTpgid
	statFlags
	statMinflt
	statCminflt
	statMajflt
	statCmajflt
	statUtime
	statStime
	statCutime
	statCstime
	statPriority
	statNice
	statNumThreads
	statItrealvalue
	statStartTime
	statVsize
	statRss
	statRsslim
	statStartCode
	statEndCode
	statStartStack
	statKstKesp
	statKstKeip
	statSignal
	statBlocked
	statSigIgnore
	statSigCatch
	statWchan
	statNswap
	statCnswap
	statExitSignal
	statProcessor
	statRtPriority
	statPolicy
	statDelayActBlkioTicks
	statGuestTime
	statCguestTime
)

// Process represents an operating system process.
type Process struct {
	PID     uint64
	User    *user.User
	Name    string // foo
	Command string // /usr/bin/foo --args

	// Alive is a flag used by ProcessMonitor to determine if it should remove
	// this process.
	Alive bool

	// Data from /proc/<pid>/stat
	Pgrp  int
	Utime uint64
	Stime uint64

	UtimeDiff uint64
	StimeDiff uint64
}

// NewProcess returns a new Process if a process is currently running on
// the system with the passed in PID.
func NewProcess(pid uint64) *Process {
	p := &Process{
		PID: pid,
	}

	if err := p.Update(); err != nil {
		return nil
	}

	if err := p.parseCmdlineFile(); err != nil {
		return nil
	}

	return p
}

// Update updates the Process from various files in /proc/<pid>. It returns an
// error if the process was unable to be updated (probably because the process
// is no longer running).
func (p *Process) Update() error {
	if err := p.statProcDir(); err != nil {
		return err
	}

	if err := p.parseStatFile(); err != nil {
		return err
	}

	return nil
}

// IsKernelThread returns whether or not Process is a kernel thread.
func (p *Process) IsKernelThread() bool {
	return p.Pgrp == 0
}

// statProcDir updates p with any information it needs from statting /proc/<pid>.
func (p *Process) statProcDir() error {
	path := path.Join("/proc", strconv.FormatUint(p.PID, 10))

	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		return err
	}

	user, err := UserByUID(strconv.FormatUint(uint64(stat.Uid), 10))
	if err != nil {
		return err
	}
	p.User = user

	return nil
}

// parseStatFile updates p with any information it needs from /proc/<pid>/stat.
func (p *Process) parseStatFile() error {
	path := path.Join("/proc", strconv.FormatUint(p.PID, 10), "stat")

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	data, err := ioutil.ReadAll(file)
	if err != nil {
		return err
	}

	line := string(data)
	values := strings.Split(line, " ")

	p.Pgrp, err = strconv.Atoi(values[statPgrp])
	if err != nil {
		panic(err)
	}

	lastUtime := p.Utime
	p.Utime, err = strconv.ParseUint(values[statUtime], 10, 64)
	if err != nil {
		panic(err)
	}
	p.UtimeDiff = p.Utime - lastUtime

	lastStime := p.Stime
	p.Stime, err = strconv.ParseUint(values[statStime], 10, 64)
	if err != nil {
		panic(err)
	}
	p.StimeDiff = p.Stime - lastStime

	return nil
}

// parseCmdlineFile sets p's Command via /proc/<pid>/cmdline.
func (p *Process) parseCmdlineFile() error {
	path := path.Join("/proc", strconv.FormatUint(p.PID, 10), "cmdline")

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	cmdline := string(data)
	p.Command = strings.TrimSpace(strings.Replace(cmdline, "\x00", " ", -1))
	p.Name = commandToName(p.Command)
	return nil
}

// commandToName takes a string in a format like "/usr/bin/foo --arguments"
// and returns its base name without arguments, "foo".
func commandToName(cmdline string) string {
	command := strings.Split(cmdline, " ")[0]
	if strings.HasSuffix(command, ":") {
		// For processes that set their name in a format like
		// "postgres: writer process" the value is returned as is.
		return cmdline
	}
	return path.Base(command)
}

// ByPID sorts by PID.
type ByPID []*Process

func (p ByPID) Len() int      { return len(p) }
func (p ByPID) Swap(i, j int) { p[i], p[j] = p[j], p[i] }
func (p ByPID) Less(i, j int) bool {
	return p[i].PID < p[j].PID
}

// ByUser sorts by the username of the processes user.
type ByUser []*Process

func (p ByUser) Len() int      { return len(p) }
func (p ByUser) Swap(i, j int) { p[i], p[j] = p[j], p[i] }
func (p ByUser) Less(i, j int) bool {
	return p[i].User.Username < p[j].User.Username
}

// ByCPU sorts by the amount of CPU time used since the last update.
type ByCPU []*Process

func (p ByCPU) Len() int      { return len(p) }
func (p ByCPU) Swap(i, j int) { p[i], p[j] = p[j], p[i] }
func (p ByCPU) Less(i, j int) bool {
	p1, p2 := p[i], p[j]
	p1Total := p1.UtimeDiff + p1.StimeDiff
	p2Total := p2.UtimeDiff + p2.StimeDiff
	if p1Total == p2Total {
		return p1.PID < p2.PID
	}
	return p1Total > p2Total
}

// ByTime sorts by the amount of CPU time used total.
type ByTime []*Process

func (p ByTime) Len() int      { return len(p) }
func (p ByTime) Swap(i, j int) { p[i], p[j] = p[j], p[i] }
func (p ByTime) Less(i, j int) bool {
	p1, p2 := p[i], p[j]
	p1Total := p1.Utime + p1.Stime
	p2Total := p2.Utime + p2.Stime
	if p1Total == p2Total {
		return p1.PID < p2.PID
	}
	return p1Total > p2Total
}

// ByName sorts by Name.
type ByName []*Process

func (p ByName) Len() int      { return len(p) }
func (p ByName) Swap(i, j int) { p[i], p[j] = p[j], p[i] }
func (p ByName) Less(i, j int) bool {
	return p[i].Name < p[j].Name
}
