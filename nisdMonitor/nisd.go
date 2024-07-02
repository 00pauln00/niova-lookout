package main

import (
	"bytes"
	"common/lookout"
	"common/requestResponseLib"
	"common/serviceDiscovery"
	"controlplane/serfAgent"
	"encoding/gob"
	"encoding/json"
	"flag"
	"net"
	"os"
	"strconv"
	"unsafe"

	"github.com/sirupsen/logrus"
)

// #include <unistd.h>
// #include <string.h>
// //#include <errno.h>
// //int usleep(useconds_t usec);
/*
#define INET_ADDRSTRLEN 16
#define UUID_LEN 37
struct nisd_config
{
  char  nisd_uuid[UUID_LEN];
  char  nisd_ipaddr[INET_ADDRSTRLEN];
  int   nisdc_addr_len;
  int   nisd_port;
};
*/
import "C"

type nisdMonitor struct {
	udpPort       string
	storageClient serviceDiscovery.ServiceDiscoveryHandler
	udpSocket     net.PacketConn
	lookout       lookout.EPContainer
	endpointRoot  *string
	httpPort      int
	ctlPath       *string
	promPath      string
	standalone    bool
	PortRangeStr  string
	//serf
	serfHandler       serfAgent.SerfAgentHandler
	agentName         string
	addr              string
	agentPort         int16
	agentRPCPort      int16
	gossipNodesPath   string
	serfLogger        string
	raftUUID          string
	PortRange         []uint16
	ServicePortRangeS uint16
	ServicePortRangeE uint16
}

// These prob need put in lookout file
var RecvdPort int
var SetTagsInterval int = 10

// No idea where to put this. what does udp pertain to? udplistener should me moved with it/
// NISD
type udpMessage struct {
	addr    net.Addr
	message []byte
}

// dont know where this should go
func usage(rc int) {
	logrus.Infof("Usage: [OPTIONS] %s\n", os.Args[0])
	flag.PrintDefaults()
	os.Exit(rc)
}

func fillNisdCStruct(UUID string, ipaddr string, port int) []byte {
	//FIXME: free the memory
	nisd_peer_config := C.struct_nisd_config{}
	C.strncpy(&(nisd_peer_config.nisd_uuid[0]), C.CString(UUID), C.ulong(len(UUID)+1))
	C.strncpy(&(nisd_peer_config.nisd_ipaddr[0]), C.CString(ipaddr), C.ulong(len(ipaddr)+1))
	nisd_peer_config.nisdc_addr_len = C.int(len(ipaddr))
	nisd_peer_config.nisd_port = C.int(port)
	returnData := C.GoBytes(unsafe.Pointer(&nisd_peer_config), C.sizeof_struct_nisd_config)
	return returnData
}

// maybe put this in niova client file
// NISD
func (handler *nisdMonitor) startClientAPI() {
	//Init niovakv client API
	handler.storageClient = serviceDiscovery.ServiceDiscoveryHandler{
		HTTPRetry: 10,
		SerfRetry: 5,
	}
	stop := make(chan int)
	go func() {
		err := handler.storageClient.StartClientAPI(stop, handler.gossipNodesPath)
		if err != nil {
			logrus.Fatal("Error while starting client API : ", err)
		}
	}()
	handler.storageClient.TillReady("", 0)
}

// NISD
func (handler *nisdMonitor) startUDPListner() {
	var err error
	handler.udpSocket, err = net.ListenPacket("udp", ":"+handler.udpPort)
	if err != nil {
		logrus.Error("UDP listner failed : ", err)
	}

	defer handler.udpSocket.Close()
	for {
		buf := make([]byte, 1024)
		_, addr, err := handler.udpSocket.ReadFrom(buf)
		if err != nil {
			continue
		}
		udpInfo := udpMessage{
			addr:    addr,
			message: buf,
		}
		go handler.getConfigNSend(udpInfo)
	}
}

// NISD
func (handler *nisdMonitor) getConfigNSend(udpInfo udpMessage) {
	//Get uuid from the byte array
	data := udpInfo.message
	uuidString := string(data[:36])

	//FIXME: Mark nisd as alive if reported dead
	handler.lookout.MarkAlive(uuidString)

	//Send config read request to PMDB server
	responseByte, _ := handler.requestPMDB(uuidString)

	//Decode response to IPAddr and Port
	responseObj := requestResponseLib.KVResponse{}
	dec := gob.NewDecoder(bytes.NewBuffer(responseByte))
	dec.Decode(&responseObj)
	var value map[string]string
	json.Unmarshal(responseObj.Value, &value)
	ipaddr := value["IP_ADDR"]
	port, _ := strconv.Atoi(value["Port"])

	//Fill C structure
	structByteArray := fillNisdCStruct(uuidString, ipaddr, port)

	//Send the data to the node
	handler.udpSocket.WriteTo(structByteArray, udpInfo.addr)
}
