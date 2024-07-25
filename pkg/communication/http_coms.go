package communication

import (
	"bytes"
	"common/httpClient"
	"common/serviceDiscovery"
	"controlplane/serfAgent"
	"encoding/gob"
	"errors"
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
	"github.com/00pauln00/niova-lookout/pkg/prometheusHandler"
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

var SetTagsInterval int = 10

// i dont think this is used anywhere
/*func (handler *ComHandler) getPortRange() error {
	var response requestResponseLib.PMDBKVResponse

	responseBytes, err := applications.RequestPMDB(handler.RaftUUID + "_Port_Range")
	if err != nil {
		logrus.Error("Request PMDB - ", err)
		return err
	}

	dec := gob.NewDecoder(bytes.NewBuffer(responseBytes))
	dec.Decode(&response)

	err = handler.getConfigData(string(response.ResultMap[handler.RaftUUID+"_Port_Range"]))
	if err != nil {
		logrus.Error("getConfigData - ", err)
	}

	return nil
}*/

// TODO: i dont think this is used anywhere
func (handler *ComHandler) findFreePort() int {
	for i := 0; i < len(handler.PortRange); i++ {
		handler.HttpPort = int(handler.PortRange[i])
		logrus.Debug("Trying to bind with - ", int(handler.HttpPort))
		check, err := net.Listen("tcp", handler.Addr+":"+strconv.Itoa(int(handler.HttpPort)))
		if err != nil {
			if strings.Contains(err.Error(), "bind") {
				continue
			} else {
				logrus.Error("Error while finding port : ", err)
				return 0
			}
		} else {
			check.Close()
			break
		}
	}
	logrus.Debug("Returning free port - ", handler.HttpPort)
	return handler.HttpPort
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

// TODO: i dont think this is used anywhere
func (handler *ComHandler) getAddrList() []string {
	var addrs []string
	for i := 0; i < len(handler.PortRange); i++ {
		addrs = append(addrs, handler.Addr+":"+strconv.Itoa(int(handler.PortRange[i])))
	}
	return addrs
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
		handler.HttpPort = 6666 // int(epc.PortRange[i]) replace this
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

// TODO: This function is not used anywhere
func (handler *ComHandler) getConfigData(config string) error {
	portRangeStart, err := strconv.Atoi(strings.Split(config, "-")[0])
	portRangeEnd, err := strconv.Atoi(strings.Split(config, "-")[1])
	if err != nil {
		return err
	}

	for i := portRangeStart; i <= portRangeEnd; i++ {
		handler.PortRange = append(handler.PortRange, uint16(i))
	}

	if len(handler.PortRange) < 3 {
		return errors.New("Not enough ports available in the specified range to start services")
	}

	return err
}

func (handler *ComHandler) customQuery(node uuid.UUID, query string) []byte {
	handler.Epc.Mutex.Lock()
	ep := handler.Epc.EpMap[node]
	handler.Epc.Mutex.Unlock()
	//If not present
	if ep == nil {
		return []byte("Specified NISD is not present")
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

// TODO: maybe put in the same file as the other http handlers
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

// TODO: MetricsHandler is a handler for prometheus metrics.. needs moving to a separate file
// TODO: application types (NISD, PMDB, etc.) require an interface which contains all the necessary methods for handling the prometheus ops
func (handler *ComHandler) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	//Split key based nisd's UUID and field
	var output string

	//Take snapshot of the EpMap
	nodeMap := make(map[uuid.UUID]*monitor.NcsiEP)
	handler.Epc.Mutex.Lock()
	for k, v := range handler.Epc.EpMap {
		nodeMap[k] = v
	}
	handler.Epc.Mutex.Unlock()

	labelMap := make(map[string]string)
	for _, node := range nodeMap {
		labelMap = applications.LoadSystemInfo(labelMap, node.EPInfo.SysInfo)
	}

	if handler.Epc.AppType == "PMDB" {
		parsedUUID, _ := uuid.Parse(handler.Epc.MonitorUUID)
		node := nodeMap[parsedUUID]
		labelMap["PMDB_UUID"] = parsedUUID.String()
		labelMap["TYPE"] = handler.Epc.AppType
		// Loading labelMap with PMDB data
		labelMap = applications.LoadPMDBLabelMap(labelMap, node.EPInfo.RaftRootEntry[0])
		// Parsing exported data
		output += prometheusHandler.GenericPromDataParser(node.EPInfo.RaftRootEntry[0], labelMap)
		// Parsing membership data
		output += handler.parseMembershipPrometheus(node.EPInfo.RaftRootEntry[0].State, node.EPInfo.RaftRootEntry[0].RaftUUID, parsedUUID.String())
		// Parsing follower data
		output += getFollowerStats(node.EPInfo.RaftRootEntry[0])
		// Parsing system info
		output += prometheusHandler.GenericPromDataParser(node.EPInfo.SysInfo, labelMap)
	} else if handler.Epc.AppType == "NISD" {
		for uuid, node := range nodeMap {
			labelMap["NISD_UUID"] = uuid.String()
			labelMap["TYPE"] = handler.Epc.AppType
			// print out node info for debugging
			logrus.Debug("NISD UUID: ", uuid)
			logrus.Debug("NISD Info: ", node.EPInfo)
			// Load labelMap with NISD data if present
			if condition := len(node.EPInfo.NISDRootEntry) == 0; condition {
				continue
			} else {
				labelMap = applications.LoadNISDLabelMap(labelMap, node.EPInfo.NISDRootEntry[0])
				// Parse NISDInfo
				output += prometheusHandler.GenericPromDataParser(node.EPInfo.NISDInformation[0], labelMap)
				// Parse NISDRootEntry
				output += prometheusHandler.GenericPromDataParser(node.EPInfo.NISDRootEntry[0], labelMap)
				// Parse nisd system info
				output += prometheusHandler.GenericPromDataParser(node.EPInfo.SysInfo, labelMap)
				// Iterate and parse each NISDChunk if populated
				for _, chunk := range node.EPInfo.NISDChunk {
					// load labelMap with NISD chunk data
					labelMap["VDEV_UUID"] = chunk.VdevUUID
					labelMap["CHUNK_NUM"] = strconv.Itoa(chunk.Number)
					// Parse each nisd chunk info
					output += prometheusHandler.GenericPromDataParser(chunk, labelMap)
				}
				//remove "VDEV_UUID" and "CHUNK_NUM" from labelMap
				delete(labelMap, "VDEV_UUID")
				delete(labelMap, "CHUNK_NUM")
				// iterate and parse each buffer set node
				for _, buffer := range node.EPInfo.BufSetNodes {
					// load labelMap with buffer set node data
					labelMap["NAME"] = buffer.Name
					// Parse each buffer set node info
					output += prometheusHandler.GenericPromDataParser(buffer, labelMap)
				}
			}
		}
		fmt.Fprintf(w, "%s", output)
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

func getFollowerStats(raftEntry applications.RaftInfo) string {
	var output string
	for indx := range raftEntry.FollowerStats {
		UUID := raftEntry.FollowerStats[indx].PeerUUID
		NextIdx := raftEntry.FollowerStats[indx].NextIdx
		PrevIdxTerm := raftEntry.FollowerStats[indx].PrevIdxTerm
		LastAckMs := raftEntry.FollowerStats[indx].LastAckMs
		output += "\n" + fmt.Sprintf(`follower_stats{uuid="%s"next_idx="%d"prev_idx_term="%d"}%d`, UUID, NextIdx, PrevIdxTerm, LastAckMs)
	}
	return output
}
