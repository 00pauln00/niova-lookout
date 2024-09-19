package applications

import (
	"fmt"
	"net/http"
	"strconv"
	"unsafe"

	"github.com/00pauln00/niova-lookout/pkg/prometheusHandler"
	"github.com/google/uuid"
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

type Nisd struct {
	uuid       uuid.UUID
	EPInfo     CtlIfOut
	membership map[string]bool
}

type NISDInfo struct {
	UUID                  string    `json:"uuid"`
	DevPath               string    `json:"dev-path"`
	ServerMode            bool      `json:"server-mode"`
	Status                string    `json:"status"`
	ReadBytes             int       `json:"dev-bytes-read" type:"counter" metric:"nisd_dev_read_bytes"`
	WriteBytes            int       `json:"dev-bytes-write" type:"counter" metric:"nisd_dev_write_bytes"`
	NetRecvBytes          int       `json:"net-bytes-recv" type:"counter" metric:"nisd_net_bytes_recv"`
	NetSendBytes          int       `json:"net-bytes-send" type:"counter" metric:"nisd_net_bytes_send"`
	DevRdSize             Histogram `json:"dev-rd-size" type:"histogram" metric:"nisd_dev_rd_size"`
	DevWrSize             Histogram `json:"dev-wr-size" type:"histogram" metric:"nisd_dev_wr_size"`
	NetRecvSize           Histogram `json:"net-recv-size" type:"histogram" metric:"nisd_net_recv_size"`
	NetSendSize           Histogram `json:"net-send-size" type:"histogram" metric:"nisd_net_send_size"`
	DevRdLatencyUsec      Histogram `json:"dev-rd-latency-usec" type:"histogram" metric:"nisd_dev_rd_latency_usec"`
	DevWrLatencyUsec      Histogram `json:"dev-wr-latency-usec" type:"histogram" metric:"nisd_dev_wr_latency_usec"`
	NetRecvLatencyUsec    Histogram `json:"net-recv-latency-usec" type:"histogram" metric:"nisd_net_recv_latency_usec"`
	NetSendLatencyUsec    Histogram `json:"net-send-latency-usec" type:"histogram" metric:"nisd_net_send_latency_usec"`
	IOReqLatencyUsec      Histogram `json:"io-req-latency-usec" type:"histogram" metric:"nisd_io_req_latency_usec"`
	NIOPWaitLatencyUsec   Histogram `json:"niop-wait-latency-usec" type:"histogram" metric:"nisd_niop_wait_latency_usec"`
	NIOPUringLatencyUsec  Histogram `json:"niop-uring-latency-usec" type:"histogram" metric:"nisd_niop_uring_latency_usec"`
	NumSubSQEs            Histogram `json:"num-sub-sqes" type:"histogram" metric:"nisd_num_sub_sqes"`
	NumWaitCQEs           Histogram `json:"num-wait-cqes" type:"histogram" metric:"nisd_num_wait_cqes"`
	UringNetResubmissions Histogram `json:"uring-net-resubmissions" type:"histogram" metric:"nisd_uring_net_resubmissions"`
	UringDevResubmissions Histogram `json:"uring-dev-resubmissions" type:"histogram" metric:"nisd_uring_dev_resubmissions"`
	UringFixedBuf         Histogram `json:"uring-fixed-buf" type:"histogram" metric:"nisd_uring_fixed_buf"`
	UringNumIOVs          Histogram `json:"uring-num-iovs" type:"histogram" metric:"nisd_uring_num_iovs"`
}

type NISDRoot struct {
	UUID                    string    `json:"uuid"`
	InstanceUUID            string    `json:"instance-uuid"`
	Status                  string    `json:"status"`
	VBlockRead              int       `json:"vblks-read" type:"counter" metric:"nisd_vblk_read"`
	VBlockHoleRead          int       `json:"vblks-hole-read" type:"gauge" metric:"nisd_vblk_hole_read"`
	VBlockWritten           int       `json:"vblks-written" type:"counter" metric:"nisd_vblk_write"`
	S3SyncSendBytes         int       `json:"s3-sync-send-bytes" type:"gauge" metric:"nisd_s3_sync_send_bytes"`
	S3SyncVBlksRead         int       `json:"s3-sync-vblks-read" type:"gauge" metric:"nisd_s3_sync_vblks_read"`
	MetablockSectorsRead    int       `json:"metablock-sectors-read" type:"counter" metric:"nisd_metablock_sectors_read"`
	MetablockSectorsWritten int       `json:"metablock-sectors-written" type:"counter" metric:"nisd_metablock_sectors_written"`
	MetablockCacheHits      int       `json:"metablock-cache-hits" type:"counter" metric:"nisd_metablock_cache_hits"`
	MetablockCacheMisses    int       `json:"metablock-cache-misses" type:"counter" metric:"nisd_metablock_cache_misses"`
	ChunkMergeQLen          int       `json:"chunk-mergeq-len" type:"gauge" metric:"nisd_chunk_mergeq_len"`
	NumReservedPblks        int       `json:"num-reserved-pblks" type:"counter" metric:"nisd_num_reserved_pblks"`
	NumReservedPblksUsed    int       `json:"num-reserved-pblks-used" type:"counter" metric:"nisd_num_reserved_pblks_used"`
	NumPblks                int       `json:"num-pblks" type:"counter" metric:"nisd_num_pblks"`
	NumPblksUsed            int       `json:"num-pblks-used" type:"counter" metric:"nisd_num_pblks_used"`
	ReleaseObjBusy          int       `json:"release-obj-busy" type:"gauge" metric:"nisd_release_obj_busy"`
	ReleaseXtraObjBusy      int       `json:"release-xtra-obj-busy" type:"gauge" metric:"nisd_release_xtra_obj_busy"`
	ReleaseObjTotal         int       `json:"release-obj-total" type:"counter" metric:"nisd_release_obj_total"`
	ReleaseXtraObjTotal     int       `json:"release-xtra-obj-total" type:"counter" metric:"nisd_release_xtra_obj_total"`
	ShallowMergeInProgress  int       `json:"shallow-merge-in-progress" type:"gauge" metric:"nisd_shallow_merge_in_progress"`
	AltName                 string    `json:"alt-name"`
	VBlkMetaReadSectors     Histogram `json:"vblk-meta-read-sectors" type:"histogram" metric:"nisd_vblk_meta_read_sectors"`
	MCIBMetaReadSectors     Histogram `json:"mcib-meta-read-sectors" type:"histogram" metric:"nisd_mcib_meta_read_sectors"`
	FullCompactionMsec      Histogram `json:"full-compaction-msec" type:"histogram" metric:"nisd_full_compaction_msec"`
	ShallowCompactionMsec   Histogram `json:"shallow-compaction-msec" type:"histogram" metric:"nisd_shallow_compaction_msec"`
}

type NISDChunkInfo struct {
	VdevUUID                   string `json:"vdev-uuid"`
	Number                     int    `json:"number"`
	Tier                       int    `json:"tier" type:"gauge" metric:"nisd_chunk_tier"`
	Type                       int    `json:"type" type:"gauge" metric:"nisd_chunk_type"`
	NumDataPblks               int    `json:"num-data-pblks" type:"counter" metric:"nisd_chunk_num_data_pblks"`
	NumMetaPblks               int    `json:"num-meta-pblks" type:"counter" metric:"nisd_chunk_num_meta_pblks"`
	NumMcibPblks               int    `json:"num-mcib-pblks" type:"counter" metric:"nisd_chunk_num_mcib_pblks"`
	NumReservedMetaPblks       int    `json:"num-reserved-meta-pblks" type:"counter" metric:"nisd_chunk_num_reserved_meta_pblks"`
	NumTypical2ReservedMbLinks int    `json:"num-typical-2-reserved-mb-links" type:"counter" metric:"nisd_chunk_num_typical_2_reserved_mb_links"`
	VblksRead                  int    `json:"vblks-read" type:"counter" metric:"nisd_chunk_vblks_read"`
	VblksWritten               int    `json:"vblks-written" type:"counter" metric:"nisd_chunk_vblks_written"`
	VblksPeerSent              int    `json:"vblks-peer-sent" type:"counter" metric:"nisd_chunk_vblks_peer_sent"`
	VblksPeerRecvd             int    `json:"vblks-peer-recvd" type:"counter" metric:"nisd_chunk_vblks_peer_recvd"`
	MergeShallowCnt            int    `json:"merge-shallow-cnt" type:"counter" metric:"nisd_chunk_merge_shallow_cnt"`
	MergeShallowStatus         string `json:"merge-shallow-status"`
	MergeFullCnt               int    `json:"merge-full-cnt" type:"counter" metric:"nisd_chunk_merge_full_cnt"`
	MergeFullCompletedCnt      int    `json:"merge-full-completed-cnt" type:"counter" metric:"nisd_chunk_merge_full_completed_cnt"`
	MergeFullStatus            string `json:"merge-full-status"`
	MergeFence                 int    `json:"merge-fence" type:"counter" metric:"nisd_chunk_merge_fence"`
	ClientRecoverySeqno        int    `json:"client-recovery-seqno" type:"counter" metric:"nisd_chunk_client_recovery_seqno"`
	MetablockSeqn              int    `json:"metablock-seqno" type:"counter" metric:"nisd_chunk_metablock_seqno"`
	S3SyncSeqno                int    `json:"s3-sync-seqno" type:"counter" metric:"nisd_chunk_s3_sync_seqno"`
	S3SyncState                string `json:"s3-sync-state"`
	NumCme                     int    `json:"num-cme" type:"counter" metric:"nisd_chunk_num_cme"`
	RefCnt                     int    `json:"ref-cnt" type:"counter" metric:"nisd_chunk_ref_cnt"`
	OooMbSyncCnt               int    `json:"ooo-mb-sync-cnt" type:"counter" metric:"nisd_chunk_ooo_mb_sync_cnt"`
	OooMbSyncNewMpblkCnt       int    `json:"ooo-mb-sync-new-mpblk-cnt" type:"counter" metric:"nisd_chunk_ooo_mb_sync_new_mpblk_cnt"`
	McibHits                   int    `json:"mcib-hits" type:"counter" metric:"nisd_chunk_mcib_hits"`
	McibMisses                 int    `json:"mcib-misses" type:"counter" metric:"nisd_chunk_mcib_misses"`
	McibSectorReads            int    `json:"mcib-sector-reads" type:"counter" metric:"nisd_chunk_mcib_sector_reads"`
	McibSectorWrites           int    `json:"mcib-sector-writes" type:"counter" metric:"nisd_chunk_mcib_sector_writes"`
	StashPblk                  int    `json:"stash-pblk" type:"counter" metric:"nisd_chunk_stash_pblk"`
	StashPblkStates            string `json:"stash-pblk-states"`
}

type BufferSetNodes struct {
	Name          string `json:"name"`
	BufSize       int    `json:"buf-size" type:"counter" metric:"buf_set_node_size"`
	NumBufs       int    `json:"num-bufs" type:"counter" metric:"buf_set_node_num_bufs"`
	InUse         int    `json:"in-use" type:"gauge" metric:"buf_set_node_in_use"`
	TotalUsed     int    `json:"total-used" type:"counter" metric:"buf_set_node_total_used"`
	MaxInUse      int    `json:"max-in-use" type:"counter" metric:"buf_set_node_max_in_use"`
	NumUserCached int    `json:"num-user-cached" type:"counter" metric:"buf_set_node_num_user_cached"`
}

func FillNisdCStruct(UUID string, ipaddr string, port int) []byte {
	//FIXME: free the memory
	nisd_peer_config := C.struct_nisd_config{}
	C.strncpy(&(nisd_peer_config.nisd_uuid[0]), C.CString(UUID), C.ulong(len(UUID)+1))
	C.strncpy(&(nisd_peer_config.nisd_ipaddr[0]), C.CString(ipaddr), C.ulong(len(ipaddr)+1))
	nisd_peer_config.nisdc_addr_len = C.int(len(ipaddr))
	nisd_peer_config.nisd_port = C.int(port)
	returnData := C.GoBytes(unsafe.Pointer(&nisd_peer_config), C.sizeof_struct_nisd_config)
	return returnData
}

func (n *Nisd) LoadNISDLabelMap(labelMap map[string]string) map[string]string {
	//labelMap["STATUS"] = nisdRootEntry.Status
	labelMap["ALT_NAME"] = n.EPInfo.NISDRootEntry[0].AltName

	return labelMap
}

func (n *Nisd) LoadSystemInfo(labelMap map[string]string) map[string]string {
	labelMap["NODE_NAME"] = n.EPInfo.SysInfo.UtsNodename
	return labelMap
}

func (n *Nisd) SetMembership(membership map[string]bool) {
	n.membership = membership
}

func (n *Nisd) GetMembership() map[string]bool {
	return n.membership
}

func (n *Nisd) GetAppName() string {
	return "NISD"
}

// TODO: Make a more refined version of this function
func (n *Nisd) GetAppDetectInfo(b bool) (string, EPcmdType) {
	return "GET /.*/.*/.*", NISDInfoOp
}

func (n *Nisd) SetCtlIfOut(c CtlIfOut) {
	n.EPInfo.NISDInformation = c.NISDInformation
	n.EPInfo.NISDRootEntry = c.NISDRootEntry
	n.EPInfo.SysInfo = c.SysInfo
	n.EPInfo.NISDChunk = c.NISDChunk
	n.EPInfo.BufSetNodes = c.BufSetNodes
}

func (n *Nisd) GetCtlIfOut() CtlIfOut {
	return n.EPInfo
}

func (n *Nisd) SetUUID(uuid uuid.UUID) {
	n.uuid = uuid
}

func (n *Nisd) GetUUID() uuid.UUID {
	return n.uuid
}

func (n *Nisd) GetAltName() string {
	if len(n.EPInfo.NISDRootEntry) == 0 {
		return ""
	}
	return n.EPInfo.NISDRootEntry[0].AltName
}

func (n *Nisd) Parse(labelMap map[string]string, w http.ResponseWriter, r *http.Request) {
	var output string
	labelMap["NISD_UUID"] = n.GetUUID().String()
	labelMap["TYPE"] = n.GetAppName()
	// print out node info for debugging
	logrus.Debug("NISD UUID: ", n.GetUUID())
	logrus.Debug("NISD Info: ", n.EPInfo)
	// Load labelMap with NISD data if present
	if condition := len(n.EPInfo.NISDRootEntry) == 0; !condition {
		labelMap = n.LoadNISDLabelMap(labelMap)
		// Parse NISDInfo
		output += prometheusHandler.GenericPromDataParser(n.EPInfo.NISDInformation[0], labelMap)
		// Parse NISDRootEntry
		output += prometheusHandler.GenericPromDataParser(n.EPInfo.NISDRootEntry[0], labelMap)
		// Parse nisd system info
		output += prometheusHandler.GenericPromDataParser(*n.EPInfo.SysInfo, labelMap)
		// Iterate and parse each NISDChunk if populated
		for _, chunk := range n.EPInfo.NISDChunk {
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
		for _, buffer := range n.EPInfo.BufSetNodes {
			// load labelMap with buffer set node data
			labelMap["NAME"] = buffer.Name
			// Parse each buffer set node info
			output += prometheusHandler.GenericPromDataParser(buffer, labelMap)
		}
	}
	fmt.Fprintf(w, "%s", output)
}
