package monitor

import (
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"

	"github.com/00pauln00/niova-lookout/pkg/monitor/applications"
	"github.com/00pauln00/niova-lookout/pkg/xlog"
)

//var HttpPort int

type LookoutHandler struct {
	PromPath  string
	HttpPort  int
	run       bool
	CTLPath   string
	Statb     syscall.Stat_t
	Epc       *EPContainer
	EpWatcher *fsnotify.Watcher
}

type lookout_state int

const (
	BOOTING lookout_state = iota
	RUNNING
	SHUTDOWN
)

// XXX move to LookoutHandler?
var lookoutState lookout_state = BOOTING

func LookoutWaitUntilReady() {
	for lookoutState != RUNNING {
		xlog.Debug("waiting for RUNNING")
		time.Sleep(1 * time.Second)
	}
}

func (h *LookoutHandler) monitor() error {
	var err error = nil
	var sleepTime time.Duration

	if lookoutState != BOOTING {
		panic("Invalid lookoutState")
	}

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

	for h.run == true {
		h.Epc.PollEPs()

		// Perform one endpoint poll before entering RUNNING mode
		if lookoutState == BOOTING {
			xlog.Debug("enter RUNNING")
			lookoutState = RUNNING
		}

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

func (h *LookoutHandler) epOutputWatcher() {
	for {
		select {
		case event := <-h.EpWatcher.Events:

			if event.Op == fsnotify.Create {
				h.processEvent(&event)
			}

			// watch for errors
		case err := <-h.EpWatcher.Errors:
			xlog.Error("EpWatcher pipe: ", err)

		}
	}
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

func (h *LookoutHandler) processEvent(event *fsnotify.Event) {

	tevnam := strings.Split(event.Name, "/") // tokenized event name
	//	var evtype = EV_TYPE_INVALID
	var pdepth = len(tevnam) - 1

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
		h.tryAdd(epUuid)
	}
}

func (h *LookoutHandler) scan() {

	if lookoutState != BOOTING {
		panic("scan() only allowed during bootup")
	}

	files, err := ioutil.ReadDir(h.CTLPath)
	if err != nil {
		xlog.Fatal("ioutil.ReadDir():", err)
	}

	for _, file := range files {
		if uuid, err := uuid.Parse(file.Name()); err == nil {
			if (h.Epc.MonitorUUID == "*") ||
				(h.Epc.MonitorUUID == uuid.String()) {
				h.tryAdd(uuid)
			}
		}
	}
}

func (h *LookoutHandler) tryAdd(epUuid uuid.UUID) {
	x := h.Epc.Lookup(epUuid)

	if x != nil {
		xlog.Infof("ep=%s (state=%s) already exists",
			epUuid.String(), x.State.String())
		return
	}

	path := h.CTLPath + "/" + epUuid.String()

	// Ensure the member is directory before proceeding
	stat, xerr := os.Lstat(path)
	if xerr != nil {
		xlog.Infof("lstat(%s) failed: %s", path, xerr)
		return
	}
	if !stat.IsDir() {
		xlog.Infof("Path %s is not a directory", path)
		return
	}

	// Create new object
	ep := NcsiEP{
		Uuid:        epUuid,
		Path:        path,
		LastReport:  time.Now(),
		LastClear:   time.Now(), //XXx remove me!
		State:       EPstateInit,
		pendingCmds: make(map[uuid.UUID]*epCommand),
		App:         &applications.Unrecognized{},
	}

	//XXXX this is all f'd up!
	//	ep.IdentifyApplicationType()
	//	ep.App.SetUUID(epUuid)
	//	ep.NiovaSvcType = ep.App.GetAppName()

	if err := h.EpWatcher.Add(ep.Path + "/output"); err != nil {
		xlog.Fatal("Watcher.Add() failed:", err)
	}

	h.Epc.UpdateEpMap(epUuid, &ep)
	//	xlog.Infof("added: UUID=%s, Path=%s, State=%s, NiovaSvcType=%s",
	//		ep.Uuid, ep.Path, ep.State.String(), ep.NiovaSvcType)

	xlog.Infof("UUID=%s, State=%s", ep.Uuid.String(), ep.State.String())
}

func (h *LookoutHandler) init() error {
	// Check the provided endpoint root path
	err := syscall.Stat(h.CTLPath, &h.Statb)
	if err != nil {
		return err
	}

	// Set path (Xxx still need to check if this is a directory or not)

	err = h.Epc.InitializeEpMap()
	if err != nil {
		return err
	}

	h.EpWatcher, err = fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	xlog.Info("fsnotify watch: ", h.CTLPath)
	err = h.EpWatcher.Add(h.CTLPath)
	if err != nil {
		h.EpWatcher.Close()
		return err
	}

	h.run = true

	go h.epOutputWatcher()

	h.scan()

	return nil
}

func (h *LookoutHandler) Start() error {
	var err error
	h.Epc.HttpQuery = make(map[string](chan []byte))

	err = h.writePromPath()
	if err != nil {
		return err
	}
	//Setup lookout
	xlog.Info("Initializing Lookout")
	err = h.init()
	if err != nil {
		xlog.Debug("Lookout Init - ", err)
		return err
	}

	//Start monitoring
	xlog.Info("Starting Monitor")
	err = h.monitor()
	if err != nil {
		xlog.Debug("Lookout Monitor - ", err)
		return err
	}

	return nil
}
