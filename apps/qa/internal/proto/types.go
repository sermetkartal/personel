// types.go contains hand-written stubs for the proto-generated Go types.
//
// These stubs exist so the QA workspace compiles before protoc is wired up.
// The field names and types match what protoc-gen-go produces for the
// proto/personel/v1/*.proto definitions.
//
// When proto generation is wired: delete this file and replace with:
//
//	//go:generate protoc --go_out=. --go-grpc_out=. proto/personel/v1/*.proto
//
// and import from the generated package instead.
package proto

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ─── Shared identifier types ────────────────────────────────────────────────

type TenantId struct{ Value []byte }
type EndpointId struct{ Value []byte }
type UserId struct{ Value []byte }
type SessionId struct{ Value []byte }
type PolicyId struct{ Value []byte }
type EventId struct{ Value []byte }
type WindowsUserSid struct{ Value string }

type AgentVersion struct {
	Major uint32
	Minor uint32
	Patch uint32
	Build string
}

type HardwareFingerprint struct {
	Blob []byte
}

// ─── Agent → Server messages ────────────────────────────────────────────────

type AgentMessage struct {
	// oneof payload
	Payload isAgentMessage_Payload
}

type isAgentMessage_Payload interface{ isAgentMessage_Payload() }

type AgentMessage_Hello struct{ Hello *Hello }
type AgentMessage_Heartbeat struct{ Heartbeat *Heartbeat }
type AgentMessage_EventBatch struct{ EventBatch *EventBatch }
type AgentMessage_PolicyAck struct{ PolicyAck *PolicyAck }
type AgentMessage_UpdateAck struct{ UpdateAck *UpdateAck }
type AgentMessage_Csr struct{ Csr *CsrSubmit }
type AgentMessage_QueueHealth struct{ QueueHealth *QueueHealth }

func (*AgentMessage_Hello) isAgentMessage_Payload()       {}
func (*AgentMessage_Heartbeat) isAgentMessage_Payload()   {}
func (*AgentMessage_EventBatch) isAgentMessage_Payload()  {}
func (*AgentMessage_PolicyAck) isAgentMessage_Payload()   {}
func (*AgentMessage_UpdateAck) isAgentMessage_Payload()   {}
func (*AgentMessage_Csr) isAgentMessage_Payload()         {}
func (*AgentMessage_QueueHealth) isAgentMessage_Payload() {}

type Hello struct {
	AgentVersion  *AgentVersion
	EndpointId    *EndpointId
	TenantId      *TenantId
	HwFingerprint *HardwareFingerprint
	ResumeCookie  []byte
	LastAckedSeq  uint64
	OsVersion     string
	AgentBuild    string
	PeDekVersion  uint32
	TmkVersion    uint32
}

type Heartbeat struct {
	SentAt        *timestamppb.Timestamp
	QueueDepth    uint64
	BlobQueueDepth uint64
	CpuPercent    float64
	RssBytes      uint64
	PolicyVersion string
}

type EventBatch struct {
	BatchId   uint64
	Events    []*Event
	BatchHmac []byte
}

type PolicyAck struct {
	PolicyVersion string
	Applied       bool
	Error         string
}

type UpdateAck struct {
	From    *AgentVersion
	To      *AgentVersion
	Success bool
	Error   string
}

type CsrSubmit struct {
	CsrDer []byte
	Reason string
}

type QueueHealth struct {
	TotalBytes         uint64
	CapacityBytes      uint64
	EvictionsSinceLast uint64
}

// ─── Server → Agent messages ────────────────────────────────────────────────

type ServerMessage struct {
	Payload isServerMessage_Payload
}

type isServerMessage_Payload interface{ isServerMessage_Payload() }

type ServerMessage_Welcome struct{ Welcome *Welcome }
type ServerMessage_PolicyPush struct{ PolicyPush *PolicyPush }
type ServerMessage_UpdateNotify struct{ UpdateNotify *UpdateNotify }
type ServerMessage_LiveViewStart struct{ LiveViewStart *LiveViewStart }
type ServerMessage_LiveViewStop struct{ LiveViewStop *LiveViewStop }
type ServerMessage_RotateCert struct{ RotateCert *RotateCert }
type ServerMessage_PinUpdate struct{ PinUpdate *PinUpdate }
type ServerMessage_Ping struct{ Ping *Ping }
type ServerMessage_CsrResponse struct{ CsrResponse *CsrResponse }
type ServerMessage_BatchAck struct{ BatchAck *BatchAck }

func (*ServerMessage_Welcome) isServerMessage_Payload()      {}
func (*ServerMessage_PolicyPush) isServerMessage_Payload()   {}
func (*ServerMessage_UpdateNotify) isServerMessage_Payload() {}
func (*ServerMessage_LiveViewStart) isServerMessage_Payload() {}
func (*ServerMessage_LiveViewStop) isServerMessage_Payload()  {}
func (*ServerMessage_RotateCert) isServerMessage_Payload()   {}
func (*ServerMessage_PinUpdate) isServerMessage_Payload()    {}
func (*ServerMessage_Ping) isServerMessage_Payload()         {}
func (*ServerMessage_CsrResponse) isServerMessage_Payload()  {}
func (*ServerMessage_BatchAck) isServerMessage_Payload()     {}

type Welcome struct {
	ServerTime    *timestamppb.Timestamp
	ServerVersion string
	AckUpToSeq    uint64
}

type PolicyPush struct {
	PolicyVersion string
	Bundle        *PolicyBundle
	Signature     []byte
	SigningKeyId  string
}

type UpdateNotify struct {
	TargetVersion      *AgentVersion
	ArtifactUrl        string
	ManifestSha256     []byte
	ManifestSignature  []byte
	Canary             bool
}

type RotateCert struct {
	NotAfter *timestamppb.Timestamp
	Reason   string
}

type PinUpdate struct {
	AllowedSpkiSha256 [][]byte
	Signature         []byte
	SigningKeyId      string
}

type CsrResponse struct {
	CertDer  []byte
	ChainDer [][]byte
	NotAfter *timestamppb.Timestamp
}

type BatchAck struct {
	BatchId       uint64
	AcceptedCount uint64
	RejectedCount uint64
}

type Ping struct {
	SentAt *timestamppb.Timestamp
}

// ─── Event messages ─────────────────────────────────────────────────────────

type Event struct {
	Meta    *EventMeta
	Payload isEvent_Payload
}

type isEvent_Payload interface{ isEvent_Payload() }

type EventMeta struct {
	EventId       *EventId
	EventType     string
	SchemaVersion uint32
	TenantId      *TenantId
	EndpointId    *EndpointId
	UserSid       *WindowsUserSid
	OccurredAt    *timestamppb.Timestamp
	ReceivedAt    *timestamppb.Timestamp
	AgentVersion  *AgentVersion
	Seq           uint64
}

// Event payload types (one per taxonomy entry).
type Event_ProcessStart struct{ ProcessStart *ProcessStart }
type Event_ProcessStop struct{ ProcessStop *ProcessStop }
type Event_ProcessForegroundChange struct{ ProcessForegroundChange *ProcessForegroundChange }
type Event_WindowTitleChanged struct{ WindowTitleChanged *WindowTitleChanged }
type Event_WindowFocusLost struct{ WindowFocusLost *WindowFocusLost }
type Event_SessionIdleStart struct{ SessionIdleStart *SessionIdleStart }
type Event_SessionIdleEnd struct{ SessionIdleEnd *SessionIdleEnd }
type Event_SessionLock struct{ SessionLock *SessionLock }
type Event_SessionUnlock struct{ SessionUnlock *SessionUnlock }
type Event_ScreenshotCaptured struct{ ScreenshotCaptured *ScreenshotCaptured }
type Event_FileCreated struct{ FileCreated *FileCreated }
type Event_FileWritten struct{ FileWritten *FileWritten }
type Event_FileDeleted struct{ FileDeleted *FileDeleted }
type Event_FileRenamed struct{ FileRenamed *FileRenamed }
type Event_FileCopied struct{ FileCopied *FileCopied }
type Event_FileRead struct{ FileRead *FileRead }
type Event_ClipboardMetadata struct{ ClipboardMetadata *ClipboardMetadata }
type Event_UsbDeviceAttached struct{ UsbDeviceAttached *UsbDeviceAttached }
type Event_UsbDeviceRemoved struct{ UsbDeviceRemoved *UsbDeviceRemoved }
type Event_NetworkFlowSummary struct{ NetworkFlowSummary *NetworkFlowSummary }
type Event_KeystrokeWindowStats struct{ KeystrokeWindowStats *KeystrokeWindowStats }
type Event_KeystrokeContentEncrypted struct{ KeystrokeContentEncrypted *KeystrokeContentEncrypted }
type Event_AppBlockedByPolicy struct{ AppBlockedByPolicy *AppBlockedByPolicy }
type Event_WebBlockedByPolicy struct{ WebBlockedByPolicy *WebBlockedByPolicy }
type Event_AgentHealthHeartbeat struct{ AgentHealthHeartbeat *AgentHealthHeartbeat }
type Event_AgentTamperDetected struct{ AgentTamperDetected *AgentTamperDetected }
type Event_LiveViewStarted struct{ LiveViewStarted *LiveViewStarted }
type Event_LiveViewStopped struct{ LiveViewStopped *LiveViewStopped }
type Event_PrintJobSubmitted struct{ PrintJobSubmitted *PrintJobSubmitted }

func (*Event_ProcessStart) isEvent_Payload()                {}
func (*Event_ProcessStop) isEvent_Payload()                 {}
func (*Event_ProcessForegroundChange) isEvent_Payload()     {}
func (*Event_WindowTitleChanged) isEvent_Payload()          {}
func (*Event_WindowFocusLost) isEvent_Payload()             {}
func (*Event_SessionIdleStart) isEvent_Payload()            {}
func (*Event_SessionIdleEnd) isEvent_Payload()              {}
func (*Event_SessionLock) isEvent_Payload()                 {}
func (*Event_SessionUnlock) isEvent_Payload()               {}
func (*Event_ScreenshotCaptured) isEvent_Payload()          {}
func (*Event_FileCreated) isEvent_Payload()                 {}
func (*Event_FileWritten) isEvent_Payload()                 {}
func (*Event_FileDeleted) isEvent_Payload()                 {}
func (*Event_FileRenamed) isEvent_Payload()                 {}
func (*Event_FileCopied) isEvent_Payload()                  {}
func (*Event_FileRead) isEvent_Payload()                    {}
func (*Event_ClipboardMetadata) isEvent_Payload()           {}
func (*Event_UsbDeviceAttached) isEvent_Payload()           {}
func (*Event_UsbDeviceRemoved) isEvent_Payload()            {}
func (*Event_NetworkFlowSummary) isEvent_Payload()          {}
func (*Event_KeystrokeWindowStats) isEvent_Payload()        {}
func (*Event_KeystrokeContentEncrypted) isEvent_Payload()   {}
func (*Event_AppBlockedByPolicy) isEvent_Payload()          {}
func (*Event_WebBlockedByPolicy) isEvent_Payload()          {}
func (*Event_AgentHealthHeartbeat) isEvent_Payload()        {}
func (*Event_AgentTamperDetected) isEvent_Payload()         {}
func (*Event_LiveViewStarted) isEvent_Payload()             {}
func (*Event_LiveViewStopped) isEvent_Payload()             {}
func (*Event_PrintJobSubmitted) isEvent_Payload()           {}

// ─── Payload structs ─────────────────────────────────────────────────────────

type ProcessStart struct {
	Pid            uint32
	ParentPid      uint32
	ImagePath      string
	ImageSha256    []byte
	CommandLineHash []byte
	Signer         string
	IntegrityLevel string
}

type ProcessStop struct {
	Pid                 uint32
	ExitCode            int32
	CpuMs               uint64
	WorkingSetPeakBytes uint64
}

type ProcessForegroundChange struct {
	PidNew           uint32
	PidPrev          uint32
	ExeNew           string
	ExePrev          string
	PrevForegroundMs uint64
}

type WindowTitleChanged struct {
	Pid                  uint32
	Hwnd                 uint64
	Title                string
	ExeName              string
	DurationMsInPrevious uint64
}

type WindowFocusLost struct {
	HwndPrev            uint64
	PidPrev             uint32
	ExePrev             string
	FocusedDurationMs   uint64
	PreviousWindowTitleEventId *EventId
}

type SessionIdleStart struct{ IdleThresholdSec uint32 }
type SessionIdleEnd struct{ IdleDurationMs uint64 }

type SessionLock struct {
	UserSid    *WindowsUserSid
	LockReason string
}

type SessionUnlock struct {
	UserSid           *WindowsUserSid
	LockedDurationMs  uint64
}

type ScreenshotCaptured struct {
	BlobRef        string
	Width          uint32
	Height         uint32
	MonitorIndex   uint32
	ForegroundExe  string
	CaptureReason  string
	BlurApplied    bool
	Sha256         []byte
	BytesSize      uint32
}

type FileCreated struct {
	Path              string
	Pid               uint32
	IsRemovableTarget bool
}

type FileWritten struct {
	Path              string
	Pid               uint32
	BytesDelta        uint64
	Sha256After       []byte
	IsRemovableTarget bool
}

type FileDeleted struct {
	Path              string
	Pid               uint32
	IsRemovableTarget bool
}

type FileRenamed struct {
	PathFrom string
	PathTo   string
	Pid      uint32
}

type FileCopied struct {
	SourcePath             string
	DestinationPath        string
	SizeBytes              uint64
	Pid                    uint32
	DestinationIsRemovable bool
	CrossVolume            bool
}

type FileRead struct {
	Path             string
	ProcessPid       uint32
	SizeBytes        uint64
	Offset           uint64
	IsRemovableSource bool
}

type ClipboardMetadata struct {
	SourcePid                  uint32
	SourceExe                  string
	ContentBytes               uint32
	ContentKind                string
	EncryptedContentAvailable  bool
	ContentBlobRef             string
}

type UsbDeviceAttached struct {
	Vid        string
	Pid        string
	SerialHash []byte
	DeviceClass string
	VendorName string
}

type UsbDeviceRemoved struct {
	Vid        string
	Pid        string
	SerialHash []byte
}

type NetworkFlowSummary struct {
	Pid       uint32
	ExeName   string
	DestIp    string
	DestPort  uint32
	Protocol  string
	BytesOut  uint64
	BytesIn   uint64
	FlowStart *timestamppb.Timestamp
	FlowEnd   *timestamppb.Timestamp
}

type KeystrokeWindowStats struct {
	Hwnd             uint64
	ExeName          string
	KeystrokeCount   uint32
	BackspaceCount   uint32
	PasteCount       uint32
	WindowDurationMs uint64
}

type KeystrokeContentEncrypted struct {
	Hwnd          uint64
	ExeName       string
	CiphertextRef string
	DekWrapRef    string
	Nonce         []byte
	Aad           []byte
	ByteLen       uint32
	KeyVersion    uint32
}

type AppBlockedByPolicy struct {
	ExeName string
	RuleId  string
	Reason  string
}

type WebBlockedByPolicy struct {
	Host       string
	RuleId     string
	BrowserExe string
}

type AgentHealthHeartbeat struct {
	CpuPercent    float64
	RssBytes      uint64
	QueueDepth    uint64
	BlobQueueDepth uint64
	DropsSinceLast uint64
	PolicyVersion  string
}

type AgentTamperDetected struct {
	CheckName   string
	Severity    int32
	DetailsHash []byte
}

type LiveViewStarted struct {
	SessionId      *SessionId
	RequestedBy    *UserId
	ApprovedBy     *UserId
	ReasonCode     string
	LivekitRoom    string
	AuditChainHead []byte
}

type LiveViewStopped struct {
	SessionId *SessionId
	EndReason string
}

type PrintJobSubmitted struct {
	PrinterName  string
	DocumentName string
	PageCount    uint32
	ByteSize     uint64
	Pid          uint32
	User         string
}

// ─── Policy types ────────────────────────────────────────────────────────────

type PolicyBundle struct {
	Version    string
	TenantId   *TenantId
	EndpointId *EndpointId
}

// ─── Live view types ─────────────────────────────────────────────────────────

type LiveViewStart struct {
	SessionId        *SessionId
	LivekitUrl       string
	LivekitRoom      string
	AgentToken       string
	NotAfter         *timestamppb.Timestamp
	ControlSignature []byte
	SigningKeyId     string
	ReasonCode       string
}

type LiveViewStop struct {
	SessionId        *SessionId
	Reason           string
	ControlSignature []byte
	SigningKeyId     string
}

// ─── gRPC service interface ──────────────────────────────────────────────────

// AgentServiceClient is the stub client interface.
// When proto is generated this will be replaced by the real generated client.
type AgentServiceClient interface {
	Stream(ctx context.Context, opts ...grpc.CallOption) (AgentService_StreamClient, error)
}

// AgentService_StreamClient is the bidi stream interface.
type AgentService_StreamClient interface {
	Send(*AgentMessage) error
	Recv() (*ServerMessage, error)
	grpc.ClientStream
}

// agentServiceClient is the stub implementation.
type agentServiceClient struct {
	cc grpc.ClientConnInterface
}

// NewAgentServiceClient returns a new stub client.
func NewAgentServiceClient(cc grpc.ClientConnInterface) AgentServiceClient {
	return &agentServiceClient{cc}
}

func (c *agentServiceClient) Stream(ctx context.Context, opts ...grpc.CallOption) (AgentService_StreamClient, error) {
	stream, err := c.cc.NewStream(ctx, &grpc.StreamDesc{
		StreamName:    "Stream",
		ServerStreams: true,
		ClientStreams: true,
	}, "/personel.v1.AgentService/Stream", opts...)
	if err != nil {
		return nil, err
	}
	return &agentServiceStreamClient{stream}, nil
}

type agentServiceStreamClient struct {
	grpc.ClientStream
}

func (x *agentServiceStreamClient) Send(m *AgentMessage) error {
	return x.ClientStream.SendMsg(m)
}

func (x *agentServiceStreamClient) Recv() (*ServerMessage, error) {
	m := new(ServerMessage)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}
