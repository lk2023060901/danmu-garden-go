// Licensed to the LF AI & Data foundation under one
// or more contributor license agreements. See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership. The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License. You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package merr

import (
	"github.com/cockroachdb/errors"
	"github.com/samber/lo"
)

const (
	CanceledCode int32 = 10000
	TimeoutCode  int32 = 10001
)

type ErrorType int32

const (
	SystemError ErrorType = 0
	InputError  ErrorType = 1
)

var ErrorTypeName = map[ErrorType]string{
	SystemError: "system_error",
	InputError:  "input_error",
}

func (err ErrorType) String() string {
	return ErrorTypeName[err]
}

// 在此处定义叶子错误。
// WARN: 新增错误时需要特别小心，
// 请优先考虑复用已有错误，再决定是否新增。
// 命名规范：Err + 相关前缀 + 错误名称。
var (
	// Service 相关错误。
	ErrServiceNotReady             = newZeusError("service not ready", 1, true) // 表示服务仍在初始化中。
	ErrServiceUnavailable          = newZeusError("service unavailable", 2, true)
	ErrServiceMemoryLimitExceeded  = newZeusError("memory limit exceeded", 3, false)
	ErrServiceTooManyRequests      = newZeusError("too many concurrent requests, queue is full", 4, true)
	ErrServiceInternal             = newZeusError("service internal error", 5, false) // Never return this error out of Zeus
	ErrServiceCrossClusterRouting  = newZeusError("cross cluster routing", 6, false)
	ErrServiceDiskLimitExceeded    = newZeusError("disk limit exceeded", 7, false)
	ErrServiceRateLimit            = newZeusError("rate limit exceeded", 8, true)
	ErrServiceQuotaExceeded        = newZeusError("quota exceeded", 9, false)
	ErrServiceUnimplemented        = newZeusError("service unimplemented", 10, false)
	ErrServiceTimeTickLongDelay    = newZeusError("time tick long delay", 11, false)
	ErrServiceResourceInsufficient = newZeusError("service resource insufficient", 12, true)

	// Collection 相关错误。
	ErrCollectionNotFound                      = newZeusError("collection not found", 100, false)
	ErrCollectionNotLoaded                     = newZeusError("collection not loaded", 101, false)
	ErrCollectionNumLimitExceeded              = newZeusError("exceeded the limit number of collections", 102, false)
	ErrCollectionNotFullyLoaded                = newZeusError("collection not fully loaded", 103, true)
	ErrCollectionLoaded                        = newZeusError("collection already loaded", 104, false)
	ErrCollectionIllegalSchema                 = newZeusError("illegal collection schema", 105, false)
	ErrCollectionOnRecovering                  = newZeusError("collection on recovering", 106, true)
	ErrCollectionVectorClusteringKeyNotAllowed = newZeusError("vector clustering key not allowed", 107, false)
	ErrCollectionReplicateMode                 = newZeusError("can't operate on the collection under standby mode", 108, false)
	ErrCollectionSchemaMismatch                = newZeusError("collection schema mismatch", 109, false)
	// Partition 相关错误。
	ErrPartitionNotFound       = newZeusError("partition not found", 200, false)
	ErrPartitionNotLoaded      = newZeusError("partition not loaded", 201, false)
	ErrPartitionNotFullyLoaded = newZeusError("partition not fully loaded", 202, true)

	// 一般容量相关错误。
	ErrGeneralCapacityExceeded = newZeusError("general capacity exceeded", 250, false)

	// ResourceGroup 相关错误。
	ErrResourceGroupNotFound      = newZeusError("resource group not found", 300, false)
	ErrResourceGroupAlreadyExist  = newZeusError("resource group already exist, but create with different config", 301, false)
	ErrResourceGroupReachLimit    = newZeusError("resource group num reach limit", 302, false)
	ErrResourceGroupIllegalConfig = newZeusError("resource group illegal config", 303, false)
	// go:deprecated
	ErrResourceGroupNodeNotEnough    = newZeusError("resource group node not enough", 304, false)
	ErrResourceGroupServiceAvailable = newZeusError("resource group service available", 305, true)

	// Replica 相关错误。
	ErrReplicaNotFound     = newZeusError("replica not found", 400, false)
	ErrReplicaNotAvailable = newZeusError("replica not available", 401, false)

	// Channel 与 Delegator 相关错误。
	ErrChannelNotFound         = newZeusError("channel not found", 500, false)
	ErrChannelLack             = newZeusError("channel lacks", 501, false)
	ErrChannelReduplicate      = newZeusError("channel reduplicates", 502, false)
	ErrChannelNotAvailable     = newZeusError("channel not available", 503, false)
	ErrChannelCPExceededMaxLag = newZeusError("channel checkpoint exceed max lag", 504, false)

	// Segment 相关错误。
	ErrSegmentNotFound    = newZeusError("segment not found", 600, false)
	ErrSegmentNotLoaded   = newZeusError("segment not loaded", 601, false)
	ErrSegmentLack        = newZeusError("segment lacks", 602, false)
	ErrSegmentReduplicate = newZeusError("segment reduplicates", 603, false)
	ErrSegmentLoadFailed  = newZeusError("segment load failed", 604, false)
	// ErrSegmentRequestResourceFailed 表示查询节点在加载 Segment 时资源耗尽（内存、磁盘或 GPU）。
	// 发生该错误时，上层协调者会将该节点标记为资源耗尽，并在一段惩罚期内不再下发新的加载任务。
	ErrSegmentRequestResourceFailed = newZeusError("segment request resource failed", 605, false)

	// Database related
	ErrDatabaseNotFound         = newZeusError("database not found", 800, false)
	ErrDatabaseNumLimitExceeded = newZeusError("exceeded the limit number of database", 801, false)
	ErrDatabaseInvalidName      = newZeusError("invalid database name", 802, false)

	// Node related
	ErrNodeNotFound        = newZeusError("node not found", 901, false)
	ErrNodeOffline         = newZeusError("node offline", 902, false)
	ErrNodeLack            = newZeusError("node lacks", 903, false)
	ErrNodeNotMatch        = newZeusError("node not match", 904, false)
	ErrNodeNotAvailable    = newZeusError("node not available", 905, false)
	ErrNodeStateUnexpected = newZeusError("node state unexpected", 906, false)

	// IO 相关错误。
	ErrIoKeyNotFound = newZeusError("key not found", 1000, false)
	ErrIoFailed      = newZeusError("IO failed", 1001, false)
	ErrIoUnexpectEOF = newZeusError("unexpected EOF", 1002, true)

	// 参数相关错误。
	ErrParameterInvalid  = newZeusError("invalid parameter", 1100, false)
	ErrParameterMissing  = newZeusError("missing parameter", 1101, false)
	ErrParameterTooLarge = newZeusError("parameter too large", 1102, false)

	// 指标相关错误。
	ErrMetricNotFound = newZeusError("metric not found", 1200, false)

	// 消息队列相关错误。
	ErrMqTopicNotFound = newZeusError("topic not found", 1300, false)
	ErrMqTopicNotEmpty = newZeusError("topic not empty", 1301, false)
	ErrMqInternal      = newZeusError("message queue internal error", 1302, false)
	ErrDenyProduceMsg  = newZeusError("deny to write the message to mq", 1303, false)

	// 权限相关错误。
	// 当用户未认证时拒绝执行操作，用户需要先完成登录。
	ErrPrivilegeNotAuthenticated = newZeusError("not authenticated", 1400, false)
	// 当用户缺少足够权限时拒绝执行操作，用户需要更高权限。
	ErrPrivilegeNotPermitted     = newZeusError("privilege not permitted", 1401, false)
	ErrPrivilegeGroupInvalidName = newZeusError("invalid privilege group name", 1402, false)

	// 别名相关错误。
	ErrAliasNotFound               = newZeusError("alias not found", 1600, false)
	ErrAliasCollectionNameConfilct = newZeusError("alias and collection name conflict", 1601, false)
	ErrAliasAlreadyExist           = newZeusError("alias already exist", 1602, false)
	ErrCollectionIDOfAliasNotFound = newZeusError("collection id of alias not found", 1603, false)

	// 字段相关错误。
	ErrFieldNotFound    = newZeusError("field not found", 1700, false)
	ErrFieldInvalidName = newZeusError("field name invalid", 1701, false)

	// 高层 RESTful API 相关错误。
	ErrNeedAuthenticate          = newZeusError("user hasn't authenticated", 1800, false)
	ErrIncorrectParameterFormat  = newZeusError("can only accept json format request", 1801, false)
	ErrMissingRequiredParameters = newZeusError("missing required parameters", 1802, false)
	ErrMarshalCollectionSchema   = newZeusError("fail to marshal collection schema", 1803, false)
	ErrInvalidInsertData         = newZeusError("fail to deal the insert data", 1804, false)
	ErrInvalidSearchResult       = newZeusError("fail to parse search result", 1805, false)
	ErrCheckPrimaryKey           = newZeusError("please check the primary key and its' type can only in [int, string]", 1806, false)
	ErrHTTPRateLimit             = newZeusError("request is rejected by limiter", 1807, true)

	// 副本同步相关错误。
	ErrDenyReplicateMessage = newZeusError("deny to use the replicate message in the normal instance", 1900, false)
	ErrInvalidMsgBytes      = newZeusError("invalid replicate msg bytes", 1901, false)
	ErrNoAssignSegmentID    = newZeusError("no assign segment id", 1902, false)
	ErrInvalidStreamObj     = newZeusError("invalid stream object", 1903, false)

	// Segcore 相关错误。
	ErrSegcore                    = newZeusError("segcore error", 2000, false)
	ErrSegcoreUnsupported         = newZeusError("segcore unsupported error", 2001, false)
	ErrSegcorePretendFinished     = newZeusError("segcore pretend finished", 2002, false)
	ErrSegcoreFollyOtherException = newZeusError("segcore folly other exception", 2037, false) // throw from segcore.
	ErrSegcoreFollyCancel         = newZeusError("segcore Future was canceled", 2038, false)   // throw from segcore.
	ErrSegcoreOutOfRange          = newZeusError("segcore out of range", 2039, false)          // throw from segcore.
	ErrSegcoreGCPNativeError      = newZeusError("segcore GCP native error", 2040, false)      // throw from segcore.
	KnowhereError                 = newZeusError("knowhere error", 2099, false)                // throw from segcore.

	// 请勿导出该错误，
	// 仅用于将未知错误转换为 zeusError，禁止在业务代码中直接使用。
	errUnexpected = newZeusError("unexpected error", (1<<16)-1, false)

	// Import 相关错误。
	ErrImportFailed = newZeusError("importing data failed", 2100, false)

	// Search/Query 相关错误。
	ErrInconsistentRequery = newZeusError("inconsistent requery result", 2200, true)

	// Compaction 相关错误。
	ErrCompactionReadDeltaLogErr                  = newZeusError("fail to read delta log", 2300, false)
	ErrIllegalCompactionPlan                      = newZeusError("compaction plan illegal", 2301, false)
	ErrCompactionPlanConflict                     = newZeusError("compaction plan conflict", 2302, false)
	// ErrClusteringCompactionClusterNotSupport 已移除，因为它属于 Zeus 集群特有行为。
	ErrClusteringCompactionCollectionNotSupport   = newZeusError("collection not support clustering compaction", 2304, false)
	ErrClusteringCompactionCollectionIsCompacting = newZeusError("collection is compacting", 2305, false)
	ErrClusteringCompactionNotSupportVector       = newZeusError("vector field clustering compaction is not supported", 2306, false)
	ErrClusteringCompactionSubmitTaskFail         = newZeusError("fail to submit task", 2307, true)
	ErrClusteringCompactionMetaError              = newZeusError("fail to update meta in clustering compaction", 2308, true)
	ErrClusteringCompactionGetCollectionFail      = newZeusError("fail to get collection in compaction", 2309, true)
	ErrCompactionResultNotFound                   = newZeusError("compaction result not found", 2310, false)
	ErrAnalyzeTaskNotFound                        = newZeusError("analyze task not found", 2311, true)
	ErrBuildCompactionRequestFail                 = newZeusError("fail to build CompactionRequest", 2312, true)
	ErrGetCompactionPlanResultFail                = newZeusError("fail to get compaction plan", 2313, true)
	ErrCompactionResult                           = newZeusError("illegal compaction results", 2314, false)
	ErrDuplicatedCompactionTask                   = newZeusError("duplicated compaction task", 2315, false)
	ErrCleanPartitionStatsFail                    = newZeusError("fail to clean partition Stats", 2316, true)

	ErrDataNodeSlotExhausted = newZeusError("datanode slot exhausted", 2401, false)

	// General
	ErrOperationNotSupported = newZeusError("unsupported operation", 3000, false)

	ErrOldSessionExists = newZeusError("old session exists", 3001, false)
)

type errorOption func(*zeusError)

func WithDetail(detail string) errorOption {
	return func(err *zeusError) {
		err.detail = detail
	}
}

func WithErrorType(etype ErrorType) errorOption {
	return func(err *zeusError) {
		err.errType = etype
	}
}

type zeusError struct {
	msg       string
	detail    string
	retriable bool
	errCode   int32
	errType   ErrorType
}

func newZeusError(msg string, code int32, retriable bool, options ...errorOption) zeusError {
	err := zeusError{
		msg:       msg,
		detail:    msg,
		retriable: retriable,
		errCode:   code,
	}

	for _, option := range options {
		option(&err)
	}
	return err
}

func (e zeusError) code() int32 {
	return e.errCode
}

func (e zeusError) Error() string {
	return e.msg
}

func (e zeusError) Detail() string {
	return e.detail
}

func (e zeusError) Is(err error) bool {
	cause := errors.Cause(err)
	if cause, ok := cause.(zeusError); ok {
		return e.errCode == cause.errCode
	}
	return false
}

type multiErrors struct {
	errs []error
}

func (e multiErrors) Unwrap() error {
	if len(e.errs) <= 1 {
		return nil
	}
	// 为了让 merr 能正确处理多重错误，
	// 需要返回多重错误的“根因”，这里约定为最后一个错误。
	if len(e.errs) == 2 {
		return e.errs[1]
	}

	return multiErrors{
		errs: e.errs[1:],
	}
}

func (e multiErrors) Error() string {
	final := e.errs[0]
	for i := 1; i < len(e.errs); i++ {
		final = errors.Wrap(e.errs[i], final.Error())
	}
	return final.Error()
}

func (e multiErrors) Is(err error) bool {
	for _, item := range e.errs {
		if errors.Is(item, err) {
			return true
		}
	}
	return false
}

func Combine(errs ...error) error {
	errs = lo.Filter(errs, func(err error, _ int) bool { return err != nil })
	if len(errs) == 0 {
		return nil
	}
	return multiErrors{
		errs,
	}
}
