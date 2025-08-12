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
	log "github.com/sirupsen/logrus"
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

var lookoutState lookout_state = BOOTING

func LookoutWaitUntilReady() {
	for lookoutState != RUNNING {
		log.Debug("waiting for RUNNING")
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
			log.Warn("LOOKOUT_SLEEP has invalid contents: defaulting to standard value '20s'\n\t\tSee ParseDuration(): (example: <num-secs>s | <num-ms>ms)")
		}
	}

	if sleepTime == 0 {
		sleepTime = 20 * time.Second
	}

	log.Info("Lookout monitor sleep time: ", sleepTime)

	for h.run == true {
		var tmp_stb syscall.Stat_t
		err = syscall.Stat(h.CTLPath, &tmp_stb)
		if err != nil {
			log.Errorf("syscall.Stat('%s'): %s", h.CTLPath, err)
			break
		}

		if tmp_stb.Mtim != h.Statb.Mtim {
			h.Statb = tmp_stb
			h.scan()
		}

		h.Epc.RefreshEndpoints()

		// Perform one endpoint scan before entering RUNNING mode
		if lookoutState == BOOTING {
			log.Debug("enter RUNNING")
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
			log.Error("EpWatcher pipe: ", err)

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

	log.Infof("event=%s, splitpath=%s, len=%d uuid-err=%s",
		event.Name, tevnam, len(tevnam), err)

	if err != nil {
		log.Error("ep uuid.Parse(): ", err)
		return
	}

	// Note that this switch may need to handle 2 items w/ the same depth
	switch pdepth {
	case EV_PATHDEPTH_EP_DATA:
		evfile := tevnam[EV_PATHDEPTH_EP_DATA]

		//temp file exclusion
		if strings.HasPrefix(evfile, ".") {
			log.Tracef("skipping ctl-interface temp file: %s",
				evfile)
			return
		}

		//Only include files contain "lookout"
		if !strings.HasPrefix(evfile, LookoutPrefixStr) {
			log.Infof(
				"event %s does not contain prefix string (%s)",
				evfile, LookoutPrefixStr)
			return
		}

		cmdUuid, xerr := uuid.Parse(evfile[len(LookoutPrefixStr):])
		if xerr != nil {
			log.Error("cmd uuid.Parse(): ", xerr)
			return
		}

		log.Infof("ev-complete: ep-uuid: %s, cmd-uuid=%s",
			epUuid.String(), cmdUuid.String())

		// XXX need to explore this!
		//h.Epc.HandleHttpQuery(evfile, uuid)
		h.Epc.Process(epUuid, cmdUuid)

	case EV_PATHDEPTH_EP_REGISTER:
		//XXX need to tryAdd the item here?
	}
}

func (h *LookoutHandler) scan() {
	files, err := ioutil.ReadDir(h.CTLPath)
	if err != nil {
		log.Fatal(err)
	}

	for _, file := range files {
		//TODO: Do we need to support removal of stale items? yes
		if uuid, err := uuid.Parse(file.Name()); err == nil {
			if (h.Epc.MonitorUUID == "*") ||
				(h.Epc.MonitorUUID == uuid.String()) {
				h.tryAdd(uuid)
			}
		}
	}
}

func (h *LookoutHandler) tryAdd(epUuid uuid.UUID) {
	lns := h.Epc.Lookup(epUuid)
	if lns == nil {
		ep := NcsiEP{
			Uuid:        epUuid,
			Path:        h.CTLPath + "/" + epUuid.String(),
			LastReport:  time.Now(),
			LastClear:   time.Now(),
			State:       EPstateInit,
			pendingCmds: make(map[uuid.UUID]*epCommand),
		}
		//XXXX this is all f'd up!
		ep.IdentifyApplicationType()
		ep.App.SetUUID(epUuid)
		ep.NiovaSvcType = ep.App.GetAppName()

		if err := h.EpWatcher.Add(ep.Path + "/output"); err != nil {
			log.Fatal("Watcher.Add() failed:", err)
		}

		h.Epc.UpdateEpMap(epUuid, &ep)
		log.Infof(
			"added: UUID=%s, Path=%s, State=%s, NiovaSvcType=%s",
			ep.Uuid, ep.Path, ep.State.String(),
			ep.NiovaSvcType)
	}
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

	log.Info("fsnotify watch: ", h.CTLPath)
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
	log.Info("Initializing Lookout")
	err = h.init()
	if err != nil {
		log.Debug("Lookout Init - ", err)
		return err
	}

	//Start monitoring
	log.Info("Starting Monitor")
	err = h.monitor()
	if err != nil {
		log.Debug("Lookout Monitor - ", err)
		return err
	}

	return nil
}
