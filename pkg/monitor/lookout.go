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
	"github.com/sirupsen/logrus"
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
		logrus.Debug("waiting for RUNNING")
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
			logrus.Warn("LOOKOUT_SLEEP has invalid contents: defaulting to standard value '20s'\n\t\tSee ParseDuration(): (example: <num-secs>s | <num-ms>ms)")
		}
	}

	if sleepTime == 0 {
		sleepTime = 20 * time.Second
	}

	logrus.Info("Lookout monitor sleep time: ", sleepTime)

	for h.run == true {
		var tmp_stb syscall.Stat_t
		err = syscall.Stat(h.CTLPath, &tmp_stb)
		if err != nil {
			logrus.Errorf("syscall.Stat('%s'): %s", h.CTLPath, err)
			break
		}

		if tmp_stb.Mtim != h.Statb.Mtim {
			h.Statb = tmp_stb
			h.scan()
		}

		h.Epc.RefreshEndpoints()

		// Perform one endpoint scan before entering RUNNING mode
		if lookoutState == BOOTING {
			logrus.Debug("enter RUNNING")
			lookoutState = RUNNING
		}

		time.Sleep(sleepTime)
	}

	return err
}

func (h *LookoutHandler) writePromPath() error {
	f, err := os.OpenFile(h.PromPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
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
				h.processInotifyEvent(&event)
			}

			// watch for errors
		case err := <-h.EpWatcher.Errors:
			logrus.Error("EpWatcher pipe: ", err)

		}
	}
}

func (h *LookoutHandler) processInotifyEvent(event *fsnotify.Event) {
	splitPath := strings.Split(event.Name, "/")
	cmpstr := splitPath[len(splitPath)-1]
	uuid, err := uuid.Parse(splitPath[len(splitPath)-3])

	if err != nil {
		logrus.Error("uuid.Parse(): ", err)
		return
	}

	//temp file exclusion
	if strings.Contains(cmpstr, ".") {
		logrus.Tracef("Skipping temp file event=%s", event.Name)
		return
	}

	//Only include files contain "lookout"
	if !strings.Contains(cmpstr, "lookout") {
		logrus.Tracef("Skipping file not containing 'lookout' event=%s",
			event.Name)
		return
	}

	logrus.Infof("ev-complete: uuid: %s, event=%s",
		uuid.String(), event.Name)

	h.Epc.HandleHttpQuery(cmpstr, uuid)
	h.Epc.ProcessEndpoint(cmpstr, uuid, event)
}

func (h *LookoutHandler) scan() {
	files, err := ioutil.ReadDir(h.CTLPath)
	if err != nil {
		logrus.Fatal(err)
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

func (h *LookoutHandler) tryAdd(uuid uuid.UUID) {
	lns := h.Epc.Lookup(uuid)
	if lns == nil {
		ep := NcsiEP{
			Uuid:        uuid,
			Path:        h.CTLPath + "/" + uuid.String(),
			LastReport:  time.Now(),
			LastClear:   time.Now(),
			Alive:       false,
			pendingCmds: make(map[string]*epCommand),
		}
		ep.IdentifyApplicationType()
		ep.App.SetUUID(uuid)
		ep.NiovaSvcType = ep.App.GetAppName()

		if err := h.EpWatcher.Add(ep.Path + "/output"); err != nil {
			logrus.Fatal("Watcher.Add() failed:", err)
		}

		h.Epc.UpdateEpMap(uuid, &ep)
		logrus.Infof(
			"added: UUID=%s, Path=%s, Alive=%t, NiovaSvcType=%s",
			ep.Uuid, ep.Path, ep.Alive,
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
	logrus.Info("Initializing Lookout")
	err = h.init()
	if err != nil {
		logrus.Debug("Lookout Init - ", err)
		return err
	}

	//Start monitoring
	logrus.Info("Starting Monitor")
	err = h.monitor()
	if err != nil {
		logrus.Debug("Lookout Monitor - ", err)
		return err
	}

	return nil
}
