package communication

import (
	"bytes"
	"common/httpClient"
	"common/serviceDiscovery"
	"controlplane/serfAgent"
	"encoding/gob"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/00pauln00/niova-lookout/pkg/monitor"
	"github.com/00pauln00/niova-lookout/pkg/monitor/applications"
	"github.com/00pauln00/niova-lookout/pkg/requestResponseLib"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

type ComHandler struct {
	Addr          string
	RecvdPort     int
	StorageClient serviceDiscovery.ServiceDiscoveryHandler
	UdpSocket     net.PacketConn
	UdpPort       string
	//serf
	SerfHandler       serfAgent.SerfAgentHandler
	AgentName         string
	AgentPort         int16
	AgentRPCPort      int16
	GossipNodesPath   string
	SerfLogger        string
	RaftUUID          string
	PortRange         []uint16
	ServicePortRangeS uint16
	ServicePortRangeE uint16
	HttpPort          int
	RetPort           *int
	Epc               *monitor.EPContainer
}

type UdpMessage struct {
	addr    net.Addr
	message []byte
}

func (handler *ComHandler) CheckHTTPLiveness() {
	var emptyByteArray []byte
	for {
		_, err := httpClient.HTTP_Request(emptyByteArray, "127.0.0.1:"+strconv.Itoa(int(handler.RecvdPort))+"/check", false)
		if err != nil {
			logrus.Error("HTTP Liveness - ", err)
		} else {
			logrus.Debug("HTTP Liveness - HTTP Server is alive")
			break
		}
		time.Sleep(1 * time.Second)
	}
}

func (handler *ComHandler) httpHandleRootRequest(w http.ResponseWriter) {
	fmt.Fprintf(w, "httpHandleRootRequest: %s\n", string(handler.Epc.JsonMarshal()))
}

func (handler *ComHandler) httpHandleUUIDRequest(w http.ResponseWriter,
	uuid uuid.UUID) {

	fmt.Fprintf(w, "httpHandleUUIDRequest: %s\n", string(handler.Epc.JsonMarshalUUID(uuid)))
}

func (handler *ComHandler) httpHandleRoute(w http.ResponseWriter, r *url.URL) {
	splitURL := strings.Split(r.String(), "/v0/")

	if len(splitURL) == 2 && len(splitURL[1]) == 0 {
		handler.httpHandleRootRequest(w)

	} else if uuid, err := uuid.Parse(splitURL[1]); err == nil {
		handler.httpHandleUUIDRequest(w, uuid)

	} else {
		fmt.Fprintln(w, "Invalid request: url", splitURL[1])
	}

}

func (handler *ComHandler) HttpHandle(w http.ResponseWriter, r *http.Request) {
	handler.httpHandleRoute(w, r.URL)
}

func (handler *ComHandler) ServeHttp() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/", handler.QueryHandle)
	mux.HandleFunc("/v0/", handler.HttpHandle)
	mux.HandleFunc("/metrics", handler.MetricsHandler)
	for i := len(handler.PortRange) - 1; i >= 0; i-- {
		l, err := net.Listen("tcp", ":"+strconv.Itoa(handler.HttpPort))
		if err != nil {
			if strings.Contains(err.Error(), "bind") {
				continue
			} else {
				logrus.Error("Error while starting lookout - ", err)
				return err
			}
		} else {
			go func() {
				*handler.RetPort = handler.HttpPort
				logrus.Info("Serving at - ", handler.HttpPort)
				http.Serve(l, mux)
			}()
		}
		break
	}
	return nil
}

func (handler *ComHandler) customQuery(node uuid.UUID, query string) []byte {
	ep := handler.Epc.Lookup(node)
	//If not present
	if ep == nil {
		return []byte("Specified App is not present")
	}

	httpID := "HTTP_" + uuid.New().String()
	handler.Epc.HttpQuery[httpID] = make(chan []byte, 2)
	ep.CtlCustomQuery(query, httpID)

	var byteOP []byte
	select {
	case byteOP = <-handler.Epc.HttpQuery[httpID]:
		break
	}
	return byteOP
}

func (handler *ComHandler) QueryHandle(w http.ResponseWriter, r *http.Request) {

	//Decode the NISD request structure
	requestBytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		logrus.Error("ioutil.ReadAll(r.Body):", err)
	}

	requestObj := requestResponseLib.LookoutRequest{}
	dec := gob.NewDecoder(bytes.NewBuffer(requestBytes))
	err = dec.Decode(&requestObj)
	if err != nil {
		logrus.Error("dec.Decode(&requestObj): ", err)
	}

	//Call the appropriate function
	output := handler.customQuery(requestObj.UUID, requestObj.Cmd)
	//Data to writer
	w.Write(output)
}

func (handler *ComHandler) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	//Take snapshot of the EpMap
	epMap := handler.Epc.TakeSnapshot()

	labelMap := make(map[string]string)
	for _, ep := range epMap {
		labelMap = applications.LoadSystemInfo(labelMap, ep.EPInfo.SysInfo)
		logrus.Debug("label map:", labelMap)
		ep.App.Parse(labelMap, w, r)
	}
}

func (handler *ComHandler) parseMembershipPrometheus(state string, raftUUID string, nodeUUID string) string {
	var output string
	membership := handler.Epc.SerfMembershipCB()
	for name, isAlive := range membership {
		var adder, status string
		if isAlive {
			adder = "1"
			status = "online"
		} else {
			adder = "0"
			status = "offline"
		}
		if nodeUUID == name {
			output += "\n" + fmt.Sprintf(`node_status{uuid="%s"state="%s"status="%s"raftUUID="%s"} %s`, name, state, status, raftUUID, adder)
		} else {
			// since we do not know the state of other nodes
			output += "\n" + fmt.Sprintf(`node_status{uuid="%s"status="%s"raftUUID="%s"} %s`, name, status, raftUUID, adder)
		}

	}
	return output
}
