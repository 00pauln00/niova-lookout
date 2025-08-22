package monitor

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"

	"github.com/00pauln00/niova-lookout/pkg/xlog"
)

//var HttpPort int

type lookoutState int

const (
	BOOTING lookoutState = iota
	RUNNING
	SHUTDOWN
)

func (s lookoutState) String() string {
	switch s {
	case BOOTING:
		return "booting"
	case RUNNING:
		return "running"
	case SHUTDOWN:
		return "shutdown"
	default:
		break
	}
	return "unknown"
}

type LookoutInotify struct {
	ifd  int
	wid  int             // for this LookoutHandler
	wmap map[int]*NcsiEP // for endpoints
}

type LookoutHandler struct {
	PromPath string
	HttpPort int
	CTLPath  string
	Statb    syscall.Stat_t
	Epc      *EPContainer
	lsofGen  uint64
	state    lookoutState
	inotify  LookoutInotify
}

func (h *LookoutHandler) LookoutWaitUntilState(s lookoutState) {
	for h.state != s {
		xlog.Debugf("waiting for %s", s.String())
		time.Sleep(1 * time.Second)
	}
}

func (h *LookoutHandler) lookoutLsof() error {
	procEntries, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}

	h.lsofGen++

	for _, e := range procEntries {
		pid := e.Name()
		if !e.IsDir() || pid[0] < '0' || pid[0] > '9' {
			continue
		}

		fdDir := filepath.Join("/proc", pid, "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue // probably not accessible
		}

		for _, fd := range fds {
			lnk, err := os.Readlink(filepath.Join(fdDir, fd.Name()))

			if err != nil {
				continue
			}

			if !strings.HasPrefix(lnk, h.CTLPath) {
				continue
			}

			dir := filepath.Dir(lnk)   // /tmp/.niova/<uuid>/input
			base := filepath.Base(dir) // <uuid>
			if u, err := uuid.Parse(base); err == nil {
				// Try to find the endpoint at this uuid
				h.Epc.LsofGenAddOrUpdateEp(h, u)

				break // Only need to process the first found
			}
		}
	}

	return nil
}

func (h *LookoutHandler) monitorLsof() {

	sleepTime := 60 * time.Second

	for h.state != SHUTDOWN {
		time.Sleep(sleepTime)

		h.lookoutLsof()
	}
}

func (h *LookoutHandler) monitor() error {
	var err error = nil
	var sleepTime time.Duration

	if h.state != BOOTING {
		panic("Invalid lookoutState")
	}

	xlog.Info("RUNNING")
	h.state = RUNNING

	sleepEnv := os.Getenv("LOOKOUT_SLEEP")
	if sleepEnv != "" {
		sleepTime, err = time.ParseDuration(sleepEnv)
		if err != nil {
			xlog.Warn("LOOKOUT_SLEEP has invalid contents: defaulting to standard value '20s'\n\t\tSee ParseDuration(): (example: <num-secs>s | <num-ms>ms)")
		}
	}

	if sleepTime == 0 {
		sleepTime = 20 * time.Second
	}

	xlog.Info("Lookout monitor sleep time: ", sleepTime)

	for h.state != SHUTDOWN {
		h.Epc.CleanEPs()
		h.Epc.PollEPs()

		time.Sleep(sleepTime)
	}

	return err
}

func (h *LookoutHandler) writePromPath() error {
	f, err := os.OpenFile(h.PromPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644)

	if err != nil {
		return err
	}
	_, err = f.WriteString(strconv.Itoa(h.HttpPort))
	if err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}

	return nil
}

type inotifyEvent struct {
	Name  string
	Event unix.InotifyEvent
}

func (h *LookoutHandler) epInotifyWatch() {

	buf := make([]byte, 4096)

	for h.state != SHUTDOWN {

		n, err := unix.Read(h.inotify.ifd, buf)
		if err != nil {
			if err == unix.EAGAIN || err == unix.EINTR {
				// no events available, just continue
				continue
			} else {
				xlog.Fatalf("unix.Read(): %v", err)
			}
		}

		var off uint32
		for off <= uint32(n-unix.SizeofInotifyEvent) {
			ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[off]))
			nameBytes := buf[off+unix.SizeofInotifyEvent : off+unix.SizeofInotifyEvent+ev.Len]

			off += unix.SizeofInotifyEvent + ev.Len

			ievent := inotifyEvent{
				Name:  string(nameBytes),
				Event: *ev, // copy struct value
			}

			xlog.Warnf("ievent=%v", ievent)

			h.processEvent(&ievent)
		}
	}

	buf = nil
}

func (h *LookoutHandler) EpWatchAdd(path string, events uint32) (int, error) {
	if h.inotify.ifd == -1 || h.inotify.wid == -1 {
		xlog.Fatal("Inotify FDs have not been initialized")
	}

	return unix.InotifyAddWatch(h.inotify.ifd, path, events)
}

func (h *LookoutHandler) EpWatchRemove(wid uint32) {
	if h.inotify.ifd == -1 || h.inotify.wid == -1 {
		xlog.Fatal("Inotify FDs have not been initialized")
	}

	unix.InotifyRmWatch(h.inotify.ifd, wid)
}

func (h *LookoutHandler) epInotifyStart() {
	h.inotify.ifd = -1
	h.inotify.wid = -1

	var err error

	h.inotify.ifd, err = unix.InotifyInit()

	xlog.FatalIfErr(err, "unix.InotifyInit(): %v", err)

	// Add watch only for CREATE events
	h.inotify.wid, err = unix.InotifyAddWatch(h.inotify.ifd, h.CTLPath,
		unix.IN_CREATE|unix.IN_ATTRIB)

	xlog.FatalIfErr(err, "unix.InotifyAddWatch(): %v", err)

	go h.epInotifyWatch()
}

const (
	EV_TYPE_INVALID     = iota // The event does match known event types
	EV_TYPE_EP_REGISTER        // New EPs entering the ctl-interface
	EV_TYPE_EP_DATA            // Existing EPs producing data
)

const (
	EV_PATHDEPTH_UUID        = 3
	EV_PATHDEPTH_EP_REGISTER = EV_PATHDEPTH_UUID
	EV_PATHDEPTH_EP_DATA     = 5
)

func (h *LookoutHandler) processEvent(event *inotifyEvent) {

	tevnam := strings.Split(event.Name, "/") // tokenized event name
	//	var evtype = EV_TYPE_INVALID
	pdepth := len(tevnam) - 1

	epUuid, err := uuid.Parse(tevnam[EV_PATHDEPTH_UUID])

	xlog.Debugf("event=%s, splitpath=%s, len=%d uuid-err=%v",
		event.Name, tevnam, len(tevnam), err)

	if err != nil {
		xlog.Error("ep uuid.Parse(): ", err)
		return
	}

	// Note that this switch may need to handle 2 items w/ the same depth
	switch pdepth {
	case EV_PATHDEPTH_EP_DATA:
		evfile := tevnam[EV_PATHDEPTH_EP_DATA]

		//temp file exclusion
		if strings.HasPrefix(evfile, ".") {
			xlog.Tracef("skipping ctl-interface temp file: %s",
				evfile)
			return
		}

		//Only include files contain "lookout"
		if !strings.HasPrefix(evfile, LookoutPrefixStr) {
			xlog.Infof(
				"event %s does not contain prefix string (%s)",
				evfile, LookoutPrefixStr)
			return
		}

		cmdUuid, xerr := uuid.Parse(evfile[len(LookoutPrefixStr):])
		if xerr != nil {
			xlog.Error("cmd uuid.Parse(): ", xerr)
			return
		}

		xlog.Infof("ev-complete: ep-uuid: %s, cmd-uuid=%s",
			epUuid.String(), cmdUuid.String())

		// XXX need to explore this!
		//h.Epc.HandleHttpQuery(evfile, uuid)
		h.Epc.Process(epUuid, cmdUuid)

	case EV_PATHDEPTH_EP_REGISTER:
		xlog.Infof("h.Epc.AddEp(): ep-uuid: %s", epUuid.String())

		h.Epc.AddEp(h, epUuid)
	}
}

func (h *LookoutHandler) Start() error {

	var err error

	// Check the provided endpoint root path
	if err = syscall.Stat(h.CTLPath, &h.Statb); err != nil {
		xlog.Error("syscall.Stat(%s): ", h.CTLPath, err)
		return err
	}

	if err = h.Epc.InitializeEpMap(); err != nil {
		xlog.Error("h.Epc.InitializeEpMap(): ", err)
		return err
	}

	h.epInotifyStart()

	h.Epc.HttpQuery = make(map[string](chan []byte))

	if err = h.writePromPath(); err != nil {
		xlog.Error("h.writePromPath(): ", err)
		return err
	}

	// Run the lsof scan to determine alive niova processes
	h.lookoutLsof()

	go h.monitorLsof()

	// Start monitoring
	if err = h.monitor(); err != nil {
		xlog.Error("h.monitor(): ", err)
		return err
	}

	return nil
}
