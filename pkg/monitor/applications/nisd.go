package applications

import (
	"fmt"
	"net/http"
	"strconv"
	"unsafe"

	"github.com/google/uuid"

	ph "github.com/00pauln00/niova-lookout/pkg/prometheusHandler"
	"github.com/00pauln00/niova-lookout/pkg/xlog"
)

// #include <unistd.h>
// #include <string.h>
// #include <stdlib.h>
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
	fromGossip bool
	address    string // IP address of the NISD instance
}

type NISDInfo struct {
	UUID                  string    `json:"uuid"`
	DevPath               string    `json:"dev-path"`
	ServerMode            bool      `json:"server-mode"`
	BuildId               string    `json:"build-id"`
	Status                string    `json:"status"`
	ReadBytes             uint64    `json:"dev-bytes-read" type:"counter" metric:"nisd_dev_read_bytes"`
	WriteBytes            uint64    `json:"dev-bytes-write" type:"counter" metric:"nisd_dev_write_bytes"`
	NetRecvBytes          uint64    `json:"net-bytes-recv" type:"counter" metric:"nisd_net_bytes_recv"`
	NetSendBytes          uint64    `json:"net-bytes-send" type:"counter" metric:"nisd_net_bytes_send"`
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
	VBlockRead              uint64    `json:"vblks-read" type:"counter" metric:"nisd_vblk_read"`
	VBlockHoleRead          uint64    `json:"vblks-hole-read" type:"gauge" metric:"nisd_vblk_hole_read"`
	VBlockWritten           uint64    `json:"vblks-written" type:"counter" metric:"nisd_vblk_write"`
	VBlockTrim              uint64    `json:"vblks-trim" type:"counter" metric:"nisd_vblk_trim"`
	S3SyncSendBytes         uint64    `json:"s3-sync-send-bytes" type:"gauge" metric:"nisd_s3_sync_send_bytes"`
	S3SyncVBlksRead         uint64    `json:"s3-sync-vblks-read" type:"gauge" metric:"nisd_s3_sync_vblks_read"`
	MetablockSectorsRead    uint64    `json:"metablock-sectors-read" type:"counter" metric:"nisd_metablock_sectors_read"`
	MetablockSectorsWritten uint64    `json:"metablock-sectors-written" type:"counter" metric:"nisd_metablock_sectors_written"`
	MetablockCacheHits      uint64    `json:"metablock-cache-hits" type:"counter" metric:"nisd_metablock_cache_hits"`
	MetablockCacheMisses    uint64    `json:"metablock-cache-misses" type:"counter" metric:"nisd_metablock_cache_misses"`
	ChunkMergeQLen          uint64    `json:"chunk-mergeq-len" type:"gauge" metric:"nisd_chunk_mergeq_len"`
	NumReservedPblks        uint64    `json:"num-reserved-pblks" type:"counter" metric:"nisd_num_reserved_pblks"`
	NumReservedPblksUsed    uint64    `json:"num-reserved-pblks-used" type:"counter" metric:"nisd_num_reserved_pblks_used"`
	NumPblks                uint64    `json:"num-pblks" type:"counter" metric:"nisd_num_pblks"`
	NumPblksUsed            uint64    `json:"num-pblks-used" type:"counter" metric:"nisd_num_pblks_used"`
	NumPblksAssignWait      uint64    `json:"num-pblks-assign-wait" type:"counter" metric:"nisd_num_pblks_assign_wait"`
	NumPblksAssignYield     uint64    `json:"num-pblks-assign-yield" type:"counter" metric:"nisd_num_pblks_assign_yield"`
	NumPblksAssignYieldErr  uint64    `json:"num-pblks-assign-yielded-err" type:"counter" metric:"nisd_num_pblks_assign_yield_err"`
	ReleaseObjBusy          uint64    `json:"release-obj-busy" type:"gauge" metric:"nisd_release_obj_busy"`
	ReleaseXtraObjBusy      uint64    `json:"release-xtra-obj-busy" type:"gauge" metric:"nisd_release_xtra_obj_busy"`
	ReleaseObjTotal         uint64    `json:"release-obj-total" type:"counter" metric:"nisd_release_obj_total"`
	ReleaseXtraObjTotal     uint64    `json:"release-xtra-obj-total" type:"counter" metric:"nisd_release_xtra_obj_total"`
	ShallowMergeInProgress  uint64    `json:"shallow-merge-in-progress" type:"gauge" metric:"nisd_shallow_merge_in_progress"`
	AltName                 string    `json:"alt-name"`
	VBlkMetaReadSectors     Histogram `json:"vblk-meta-read-sectors" type:"histogram" metric:"nisd_vblk_meta_read_sectors"`
	MCIBMetaReadSectors     Histogram `json:"mcib-meta-read-sectors" type:"histogram" metric:"nisd_mcib_meta_read_sectors"`
	MWCPoolFree             uint64    `json:"mwc-pool-free" type:"counter" metric:"nisd_mwc_pool_free"`
	MWCPoolAlloc            uint64    `json:"mwc-pool-alloc" type:"gauge" metric:"nisd_mwc_pool_alloc"`
	MWCPoolMaxAlloc         uint64    `json:"mwc-pool-max-alloc" type:"gauge" metric:"nisd_mwc_pool_max_alloc"`
	MWCPoolBufWaiters       uint64    `json:"mwc-pool-buf-waiters" type:"gauge" metric:"nisd_mwc_pool_buf_waiters"`
}

type NISDChunkInfo struct {
	VdevUUID                   string `json:"vdev-uuid"`
	Number                     uint64 `json:"number"`
	Tier                       uint64 `json:"tier" type:"gauge" metric:"nisd_chunk_tier"`
	Type                       uint64 `json:"type" type:"gauge" metric:"nisd_chunk_type"`
	NumDataPblks               uint64 `json:"num-data-pblks" type:"counter" metric:"nisd_chunk_num_data_pblks"`
	NumMetaPblks               uint64 `json:"num-meta-pblks" type:"counter" metric:"nisd_chunk_num_meta_pblks"`
	NumMcibPblks               uint64 `json:"num-mcib-pblks" type:"counter" metric:"nisd_chunk_num_mcib_pblks"`
	NumReservedMetaPblks       uint64 `json:"num-reserved-meta-pblks" type:"counter" metric:"nisd_chunk_num_reserved_meta_pblks"`
	NumTypical2ReservedMbLinks uint64 `json:"num-typical-2-reserved-mb-links" type:"counter" metric:"nisd_chunk_num_typical_2_reserved_mb_links"`
	VblksRead                  uint64 `json:"vblks-read" type:"counter" metric:"nisd_chunk_vblks_read"`
	VblksWritten               uint64 `json:"vblks-written" type:"counter" metric:"nisd_chunk_vblks_written"`
	VblksPeerSent              uint64 `json:"vblks-peer-sent" type:"counter" metric:"nisd_chunk_vblks_peer_sent"`
	VblksPeerRecvd             uint64 `json:"vblks-peer-recvd" type:"counter" metric:"nisd_chunk_vblks_peer_recvd"`
	MergeShallowCnt            uint64 `json:"merge-shallow-cnt" type:"counter" metric:"nisd_chunk_merge_shallow_cnt"`
	MergeShallowStatus         string `json:"merge-shallow-status"`
	MergeFullCnt               uint64 `json:"merge-full-cnt" type:"counter" metric:"nisd_chunk_merge_full_cnt"`
	MergeFullCompletedCnt      uint64 `json:"merge-full-completed-cnt" type:"counter" metric:"nisd_chunk_merge_full_completed_cnt"`
	MergeFullStatus            string `json:"merge-full-status"`
	MergeFence                 int64  `json:"merge-fence" type:"counter" metric:"nisd_chunk_merge_fence"`
	ClientRecoverySeqno        int64  `json:"client-recovery-seqno" type:"counter" metric:"nisd_chunk_client_recovery_seqno"`
	MetablockSeqn              int64  `json:"metablock-seqno" type:"counter" metric:"nisd_chunk_metablock_seqno"`
	S3SyncSeqno                int64  `json:"s3-sync-seqno" type:"counter" metric:"nisd_chunk_s3_sync_seqno"`
	S3SyncState                string `json:"s3-sync-state"`
	NumCme                     uint64 `json:"num-cme" type:"counter" metric:"nisd_chunk_num_cme"`
	RefCnt                     uint64 `json:"ref-cnt" type:"counter" metric:"nisd_chunk_ref_cnt"`
	OooMbSyncCnt               uint64 `json:"ooo-mb-sync-cnt" type:"counter" metric:"nisd_chunk_ooo_mb_sync_cnt"`
	OooMbSyncNewMpblkCnt       uint64 `json:"ooo-mb-sync-new-mpblk-cnt" type:"counter" metric:"nisd_chunk_ooo_mb_sync_new_mpblk_cnt"`
	McibHits                   uint64 `json:"mcib-hits" type:"counter" metric:"nisd_chunk_mcib_hits"`
	McibMisses                 uint64 `json:"mcib-misses" type:"counter" metric:"nisd_chunk_mcib_misses"`
	McibSectorReads            uint64 `json:"mcib-sector-reads" type:"counter" metric:"nisd_chunk_mcib_sector_reads"`
	McibSectorWrites           uint64 `json:"mcib-sector-writes" type:"counter" metric:"nisd_chunk_mcib_sector_writes"`
	StashPblk                  int64  `json:"stash-pblk" type:"counter" metric:"nisd_chunk_stash_pblk"`
	StashPblkStates            string `json:"stash-pblk-states"`
}

type BufferSetNodes struct {
	Name          string `json:"name"`
	BufSize       uint64 `json:"buf-size" type:"counter" metric:"buf_set_node_size"`
	NumBufs       uint64 `json:"num-bufs" type:"counter" metric:"buf_set_node_num_bufs"`
	InUse         uint64 `json:"in-use" type:"gauge" metric:"buf_set_node_in_use"`
	TotalUsed     uint64 `json:"total-used" type:"counter" metric:"buf_set_node_total_used"`
	MaxInUse      uint64 `json:"max-in-use" type:"counter" metric:"buf_set_node_max_in_use"`
	NumUserCached uint64 `json:"num-user-cached" type:"counter" metric:"buf_set_node_num_user_cached"`
}

type TaskInfo struct {
	Type           string `json:"type"`
	ExecCnt        uint64 `json:"exec-cnt" type:"counter" metric:"task_exec_cnt"`
	YieldCnt       uint64 `json:"yield-cnt" type:"counter" metric:"task_yield_cnt"`
	Running        uint64 `json:"running" type:"gauge" metric:"task_num_running"`
	MaxStack       uint64 `json:"max-save-stack-size" type:"gauge" metric:"task_max_stack"`
	WaitCoBuf      uint64 `json:"wait-co-buf" type:"gauge" metric:"task_wait_co_buf"`
	WaitBulkBuf    uint64 `json:"wait-bulk-buf" type:"gauge" metric:"task_wait_bulk_buf"`
	WaitPblk       uint64 `json:"wait-pblk" type:"gauge" metric:"task_wait_pblk"`
	WaitIO         uint64 `json:"wait-io" type:"gauge" metric:"task_wait_io"`
	WaitUser       uint64 `json:"wait-user-resource" type:"gauge" metric:"task_wait_user"`
	WaitUserNet    uint64 `json:"wait-user-net-resource" type:"gauge" metric:"task_wait_user_net"`
	WaitMergePre   uint64 `json:"wait-merge-preempted" type:"gauge" metric:"task_wait_merge_pre"`
	WaitMBCSync    uint64 `json:"wait-mbc-sync" type:"gauge" metric:"task_wait_mbc_sync"`
	WaitMBCBuf     uint64 `json:"wait-mbc-buf" type:"gauge" metric:"task_wait_mbc_buf"`
	WaitSerialized uint64 `json:"wait-serialized" type:"gauge" metric:"task_wait_serial"`
	WaitS3onDemand uint64 `json:"wait-s3-on-demand" type:"gauge" metric:"task_wait_s3"`
	WaitWCBuf      uint64 `json:"wait-wc-buf" type:"gauge" metric:"task_wait_wc_buf"`
	WaitWCPresync  uint64 `json:"wait-wc-presync" type:"gauge" metric:"task_wait_wc_presync"`
	WaitOther      uint64 `json:"wait-other" type:"gauge" metric:"task_wait_other"`
}

func FillNisdCStruct(UUID string, ipaddr string, port int) []byte {
	nisd_peer_config := C.struct_nisd_config{}
	uuidCStr := C.CString(UUID)
	defer C.free(unsafe.Pointer(uuidCStr))
	ipaddrCStr := C.CString(ipaddr)
	defer C.free(unsafe.Pointer(ipaddrCStr))
	C.strncpy(&(nisd_peer_config.nisd_uuid[0]), uuidCStr,
		C.ulong(len(UUID)+1))
	C.strncpy(&(nisd_peer_config.nisd_ipaddr[0]), ipaddrCStr,
		C.ulong(len(ipaddr)+1))
	nisd_peer_config.nisdc_addr_len = C.int(len(ipaddr))
	nisd_peer_config.nisd_port = C.int(port)
	returnData := C.GoBytes(unsafe.Pointer(&nisd_peer_config),
		C.sizeof_struct_nisd_config)
	return returnData
}

func (n *Nisd) LoadNISDLabelMap(labelMap map[string]string) map[string]string {
	//labelMap["STATUS"] = nisdRootEntry.Status
	labelMap["ALT_NAME"] = n.EPInfo.NISDRootEntry[0].AltName

	return labelMap
}

func (n *Nisd) LoadSystemInfo(labelMap map[string]string) map[string]string {

	if n.EPInfo.SysInfo == nil {
		xlog.Warnf("NISD %s is not ready", n.GetUUID().String())
		return nil
	}

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
	n.EPInfo.NISD = c.NISD
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

func (n *Nisd) Parse(labels map[string]string, w http.ResponseWriter,
	r *http.Request) {
	var out string
	labels["NISD_UUID"] = n.GetUUID().String()
	labels["TYPE"] = n.GetAppName()

	// print out node info for debugging
	xlog.Debugf("NISD UUID=%s ", n.GetUUID().String())

	// Load labels with NISD data if present
	if condition := len(n.EPInfo.NISDRootEntry) == 0; !condition {
		labels = n.LoadNISDLabelMap(labels)

		out += ph.GenericPromDataParser(n.EPInfo.NISD[0], labels)
		out += ph.GenericPromDataParser(n.EPInfo.NISDRootEntry[0],
			labels)
		out += ph.GenericPromDataParser(*n.EPInfo.SysInfo, labels)

		// Iterate and parse each NISDChunk if populated
		for _, chunk := range n.EPInfo.NISDChunk {
			// load labels with NISD chunk data
			labels["VDEV_UUID"] = chunk.VdevUUID
			labels["CHUNK_NUM"] =
				strconv.FormatUint(chunk.Number, 10)

			// Parse each nisd chunk info
			out += ph.GenericPromDataParser(chunk, labels)
		}
		//remove "VDEV_UUID" and "CHUNK_NUM" from labels
		delete(labels, "VDEV_UUID")
		delete(labels, "CHUNK_NUM")
		// iterate and parse each buffer set node
		for _, buffer := range n.EPInfo.BufSetNodes {
			// load labels with buffer set node data
			labels["NAME"] = buffer.Name
			// Parse each buffer set node info
			out += ph.GenericPromDataParser(buffer, labels)
		}
	}
	fmt.Fprintf(w, "%s", out)
}

func (n *Nisd) IsMonitoringEnabled() bool {
	return true
}
